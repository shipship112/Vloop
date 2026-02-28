package video

import (
	"feedsystem_video_go/internal/middleware/jwt"

	"github.com/gin-gonic/gin"
)

// LikeHandler 点赞处理器，负责处理点赞相关的HTTP请求
type LikeHandler struct {
	service *LikeService // 点赞服务层
}

// NewLikeHandler 创建点赞处理器实例
func NewLikeHandler(service *LikeService) *LikeHandler {
	return &LikeHandler{service: service}
}

// Like 点赞视频接口
// 路由：POST /like/like
// 功能：用户点赞指定视频（支持异步更新点赞数）
// 请求体：{"video_id": 视频ID}
func (lh *LikeHandler) Like(c *gin.Context) {
	// 1. 解析JSON请求体
	var req LikeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验视频ID
	if req.VideoID <= 0 {
		c.JSON(400, gin.H{"error": "video_id is required"})
		return
	}

	// 3. 从JWT中间件获取当前登录用户ID
	accountID, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 4. 构造点赞对象
	like := &Like{
		VideoID:   req.VideoID, // 视频ID
		AccountID: accountID,  // 用户ID
	}

	// 5. 调用Service层处理点赞（含MQ异步更新点赞数）
	if err := lh.service.Like(c.Request.Context(), like); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回成功消息
	c.JSON(200, gin.H{"message": "like success"})
}

// Unlike 取消点赞接口
// 路由：POST /like/unlike
// 功能：用户取消点赞指定视频（支持异步更新点赞数）
// 请求体：{"video_id": 视频ID}
func (lh *LikeHandler) Unlike(c *gin.Context) {
	// 1. 解析JSON请求体
	var req LikeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验视频ID
	if req.VideoID <= 0 {
		c.JSON(400, gin.H{"error": "video_id is required"})
		return
	}

	// 3. 从JWT中间件获取当前登录用户ID
	accountID, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 4. 构造点赞对象
	like := &Like{
		VideoID:   req.VideoID, // 视频ID
		AccountID: accountID,  // 用户ID
	}

	// 5. 调用Service层处理取消点赞（含MQ异步更新点赞数）
	if err := lh.service.Unlike(c.Request.Context(), like); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回成功消息
	c.JSON(200, gin.H{"message": "unlike success"})
}

// IsLiked 查询是否已点赞接口
// 路由：POST /like/is-liked
// 功能：查询当前用户是否已点赞指定视频
// 请求体：{"video_id": 视频ID}
// 返回：{"is_liked": true/false}
func (lh *LikeHandler) IsLiked(c *gin.Context) {
	// 1. 解析JSON请求体
	var req LikeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验视频ID
	if req.VideoID <= 0 {
		c.JSON(400, gin.H{"error": "video_id is required"})
		return
	}

	// 3. 从JWT中间件获取当前登录用户ID
	accountID, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 4. 调用Service层查询是否已点赞
	isLiked, err := lh.service.IsLiked(c.Request.Context(), req.VideoID, accountID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 5. 返回点赞状态
	c.JSON(200, gin.H{"is_liked": isLiked})
}

// ListMyLikedVideos 查询我点赞的视频列表接口
// 路由：POST /like/my-liked-videos
// 功能：查询当前用户点赞的所有视频
func (lh *LikeHandler) ListMyLikedVideos(c *gin.Context) {
	// 1. 从JWT中间件获取当前登录用户ID
	accountID, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 调用Service层查询点赞的视频列表
	videos, err := lh.service.ListLikedVideos(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 3. 返回视频列表
	c.JSON(200, videos)
}
