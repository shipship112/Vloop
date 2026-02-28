// Package http 定义了 HTTP 路由和依赖注入
// 这里的关键作用是：初始化所有模块的 Service，并注入依赖（数据库、缓存、MQ）
package http

import (
	"feedsystem_video_go/internal/account"
	"feedsystem_video_go/internal/feed"
	"feedsystem_video_go/internal/middleware/jwt"
	"feedsystem_video_go/internal/middleware/rabbitmq"
	rediscache "feedsystem_video_go/internal/middleware/redis"
	"feedsystem_video_go/internal/social"
	"feedsystem_video_go/internal/video"
	"log"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetRouter 设置所有 HTTP 路由，并初始化依赖注入
//
// 依赖注入流程（以点赞模块为例）：
//   1. NewRabbitMQ()    → 创建 RabbitMQ 基础连接
//   2. NewLikeMQ(rmq)   → 创建点赞 MQ（声明交换机、队列、绑定）
//   3. NewLikeRepo(db)  → 创建点赞仓储（数据库操作）
//   4. NewLikeService() → 创建点赞服务（注入 repo、cache、likeMQ、popularityMQ）
//   5. NewLikeHandler() → 创建点赞处理器（注入 service）
//   6. 设置路由        → Handler 对外提供 HTTP 接口
//
// 参数：
//   db    - GORM 数据库连接
//   cache - Redis 缓存客户端（可能为 nil）
//   rmq   - RabbitMQ 基础连接（可能为 nil）
//
// 返回：
//   *gin.Engine - Gin 路由引擎
func SetRouter(db *gorm.DB, cache *rediscache.Client, rmq *rabbitmq.RabbitMQ) *gin.Engine {
	r := gin.Default()

	// 静态文件服务：提供上传的图片和视频访问
	// 访问路径：http://localhost:8080/static/xxx.jpg
	r.Static("/static", "./.run/uploads")
	// account
	accountRepository := account.NewAccountRepository(db)
	accountService := account.NewAccountService(accountRepository, cache)
	accountHandler := account.NewAccountHandler(accountService)
	accountGroup := r.Group("/account")
	{
		accountGroup.POST("/register", accountHandler.CreateAccount)
		accountGroup.POST("/login", accountHandler.Login)
		accountGroup.POST("/changePassword", accountHandler.ChangePassword)
		accountGroup.POST("/findByID", accountHandler.FindByID)
		accountGroup.POST("/findByUsername", accountHandler.FindByUsername)
	}
	protectedAccountGroup := accountGroup.Group("")
	protectedAccountGroup.Use(jwt.JWTAuth(accountRepository, cache))
	{
		protectedAccountGroup.POST("/logout", accountHandler.Logout)
		protectedAccountGroup.POST("/rename", accountHandler.Rename)
	}
	// ========== 视频模块 ==========
	// 初始化视频仓储
	videoRepository := video.NewVideoRepository(db)

	// 初始化热度 MQ（用于异步更新视频热度）
	// NewPopularityMQ 内部会：
	//   1. 声明 Exchange("video.popularity.events")
	//   2. 声明 Queue("video.popularity.events")
	//   3. 绑定：Routing Key "video.popularity.*" → Queue
	// 如果 RabbitMQ 不可用，popularityMQ 会被设为 nil
	popularityMQ, err := rabbitmq.NewPopularityMQ(rmq)
	if err != nil {
		log.Printf("PopularityMQ init failed (mq disabled): %v", err)
		popularityMQ = nil
	}

	// 初始化视频服务（注入 cache 和 popularityMQ）
	videoService := video.NewVideoService(videoRepository, cache, popularityMQ)
	videoHandler := video.NewVideoHandler(videoService, accountService)

	// 设置视频路由
	videoGroup := r.Group("/video")
	{
		videoGroup.POST("/listByAuthorID", videoHandler.ListByAuthorID)
		videoGroup.POST("/getDetail", videoHandler.GetDetail)
	}
	protectedVideoGroup := videoGroup.Group("")
	protectedVideoGroup.Use(jwt.JWTAuth(accountRepository, cache))
	{
		protectedVideoGroup.POST("/uploadVideo", videoHandler.UploadVideo)
		protectedVideoGroup.POST("/uploadCover", videoHandler.UploadCover)
		protectedVideoGroup.POST("/publish", videoHandler.PublishVideo)
	}

	// ========== 点赞模块 ==========
	// 初始化点赞 MQ（用于异步处理点赞/取消点赞事件）
	// NewLikeMQ 内部会：
	//   1. 声明 Exchange("like.events")
	//   2. 声明 Queue("like.events")
	//   3. 绑定：Routing Key "like.*" → Queue
	likeMQ, err := rabbitmq.NewLikeMQ(rmq)
	if err != nil {
		log.Printf("LikeMQ init failed (mq disabled): %v", err)
		likeMQ = nil
	}

	// 初始化点赞仓储
	likeRepository := video.NewLikeRepository(db)

	// 初始化点赞服务（注入 repo、cache、likeMQ、popularityMQ）
	// 注意：likeMQ 用于异步处理点赞记录，popularityMQ 用于异步更新热度
	likeService := video.NewLikeService(likeRepository, videoRepository, cache, likeMQ, popularityMQ)
	likeHandler := video.NewLikeHandler(likeService)

	// 设置点赞路由（全部需要登录）
	likeGroup := r.Group("/like")
	protectedLikeGroup := likeGroup.Group("")
	protectedLikeGroup.Use(jwt.JWTAuth(accountRepository, cache))
	{
		protectedLikeGroup.POST("/like", likeHandler.Like)                // 点赞
		protectedLikeGroup.POST("/unlike", likeHandler.Unlike)            // 取消点赞
		protectedLikeGroup.POST("/isLiked", likeHandler.IsLiked)          // 查询是否点赞
		protectedLikeGroup.POST("/listMyLikedVideos", likeHandler.ListMyLikedVideos) // 查询点赞列表
	}

	// ========== 评论模块 ==========
	// 初始化评论仓储
	commentRepository := video.NewCommentRepository(db)

	// 初始化评论 MQ（用于异步处理发布/删除评论事件）
	// NewCommentMQ 内部会：
	//   1. 声明 Exchange("comment.events")
	//   2. 声明 Queue("comment.events")
	//   3. 绑定：Routing Key "comment.*" → Queue
	commentMQ, err := rabbitmq.NewCommentMQ(rmq)
	if err != nil {
		log.Printf("CommentMQ init failed (mq disabled): %v", err)
		commentMQ = nil
	}

	// 初始化评论服务（注入 repo、cache、commentMQ、popularityMQ）
	commentService := video.NewCommentService(commentRepository, videoRepository, cache, commentMQ, popularityMQ)
	commentHandler := video.NewCommentHandler(commentService, accountService)

	// 设置评论路由
	commentGroup := r.Group("/comment")
	{
		commentGroup.POST("/listAll", commentHandler.GetAllComments) // 公开接口：查询评论
	}
	protectedCommentGroup := commentGroup.Group("")
	protectedCommentGroup.Use(jwt.JWTAuth(accountRepository, cache))
	{
		protectedCommentGroup.POST("/publish", commentHandler.PublishComment) // 发布评论（需要登录）
		protectedCommentGroup.POST("/delete", commentHandler.DeleteComment)   // 删除评论（需要登录）
	}

	// ========== 关注模块 ==========
	// 初始化关注 MQ（用于异步处理关注/取关事件）
	// NewSocialMQ 内部会：
	//   1. 声明 Exchange("social.events")
	//   2. 声明 Queue("social.events")
	//   3. 绑定：Routing Key "social.*" → Queue
	socialMQ, err := rabbitmq.NewSocialMQ(rmq)
	if err != nil {
		log.Printf("SocialMQ init failed (mq disabled): %v", err)
		socialMQ = nil
	}

	// 初始化关注仓储和服务
	socialRepository := social.NewSocialRepository(db)
	socialService := social.NewSocialService(socialRepository, accountRepository, socialMQ)
	socialHandler := social.NewSocialHandler(socialService)

	// 设置关注路由（全部需要登录）
	socialGroup := r.Group("/social")
	protectedSocialGroup := socialGroup.Group("")
	protectedSocialGroup.Use(jwt.JWTAuth(accountRepository, cache))
	{
		protectedSocialGroup.POST("/follow", socialHandler.Follow)                // 关注
		protectedSocialGroup.POST("/unfollow", socialHandler.Unfollow)            // 取关
		protectedSocialGroup.POST("/getAllFollowers", socialHandler.GetAllFollowers) // 查询粉丝列表
		protectedSocialGroup.POST("/getAllVloggers", socialHandler.GetAllVloggers)   // 查询关注列表
	}
	// feed
	feedRepository := feed.NewFeedRepository(db)
	feedService := feed.NewFeedService(feedRepository, likeRepository, cache)
	feedHandler := feed.NewFeedHandler(feedService)
	feedGroup := r.Group("/feed")
	feedGroup.Use(jwt.SoftJWTAuth(accountRepository, cache))
	{
		feedGroup.POST("/listLatest", feedHandler.ListLatest)
		feedGroup.POST("/listLikesCount", feedHandler.ListLikesCount)
		feedGroup.POST("/listByPopularity", feedHandler.ListByPopularity)
	}
	protectedFeedGroup := feedGroup.Group("")
	protectedFeedGroup.Use(jwt.JWTAuth(accountRepository, cache))
	{
		protectedFeedGroup.POST("/listByFollowing", feedHandler.ListByFollowing)
	}
	return r
}
