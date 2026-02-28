// Package feed 定义了 Feed 流的 HTTP 处理器
// 职责：接收 HTTP 请求，解析参数，调用 Service 层，返回响应
package feed

import (
	"feedsystem_video_go/internal/middleware/jwt"
	"time"

	"github.com/gin-gonic/gin"
)

// FeedHandler Feed 流处理器
type FeedHandler struct {
	service *FeedService // Feed 流服务层
}

// NewFeedHandler 创建 Feed 处理器实例
func NewFeedHandler(service *FeedService) *FeedHandler {
	return &FeedHandler{service: service}
}

// ============ 最新视频接口 ============

// ListLatest 查询最新视频（公开接口，不需要登录）
//
// 路由：POST /feed/listLatest
// 功能：按创建时间降序返回最新发布的视频
// 场景：用户打开首页，看到最新发布的视频
//
// 请求示例：
//   {
//     "limit": 10,
//     "latest_time": 0  // 第一页传 0
//   }
//
// 响应示例：
//   {
//     "video_list": [...],
//     "next_time": 1640000000,
//     "has_more": true
//   }
//
// 业务流程：
//   1. 解析请求参数（limit、latest_time）
//   2. 获取当前用户 ID（可选，用于查询点赞状态）
//   3. 调用 Service 层查询视频
//   4. 返回响应
//
// 参数：
//   c - Gin 上下文
func (f *FeedHandler) ListLatest(c *gin.Context) {
	// 1. 解析请求参数
	var req ListLatestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验并限制 limit（防止一次查询过多数据）
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 10 // 默认值
	}

	// 3. 转换游标时间戳（Unix 时间戳 → time.Time）
	var latestTime time.Time
	if req.LatestTime > 0 {
		latestTime = time.Unix(req.LatestTime, 0)
	}

	// 4. 获取当前用户 ID（用于查询点赞状态）
	// 注意：这个接口可以匿名访问，未登录时 viewerAccountID = 0
	viewerAccountID, err := jwt.GetAccountID(c)
	if err != nil {
		viewerAccountID = 0
	}

	// 5. 调用 Service 层查询视频
	feedItems, err := f.service.ListLatest(c.Request.Context(), req.Limit, latestTime, viewerAccountID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回响应
	c.JSON(200, feedItems)
}

// ============ 点赞排行接口 ============

// ListLikesCount 按点赞数查询视频（公开接口，不需要登录）
//
// 路由：POST /feed/listLikesCount
// 功能：按点赞数降序返回视频（复合游标分页）
// 场景：用户查看点赞最多的视频
//
// 请求示例：
//   {
//     "limit": 10,
//     "likes_count_before": 1000,  // 上一页最后一条视频的点赞数
//     "id_before": 123              // 上一页最后一条视频的 ID
//   }
//
// 响应示例：
//   {
//     "video_list": [...],
//     "next_likes_count_before": 800,
//     "next_id_before": 456,
//     "has_more": true
//   }
//
// 复合游标说明：
//   使用点赞数 + ID 作为游标，解决点赞数相同的情况
//   例如：点赞数都是 1000 的视频，通过 ID 区分
//
// 参数：
//   c - Gin 上下文
func (f *FeedHandler) ListLikesCount(c *gin.Context) {
	// 1. 解析请求参数
	var req ListLikesCountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验并限制 limit
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 10 // 默认值
	}

	// 3. 解析复合游标（点赞数 + ID）
	var cursor *LikesCountCursor
	if req.LikesCountBefore != nil || req.IDBefore != nil {
		// 校验：两个字段必须同时提供或同时为空
		if req.LikesCountBefore == nil || req.IDBefore == nil {
			c.JSON(400, gin.H{"error": "likes_count_before and id_before must be provided together"})
			return
		}

		likesCountBefore := *req.LikesCountBefore
		idBefore := *req.IDBefore

		// 校验：点赞数不能为负数
		if likesCountBefore < 0 {
			c.JSON(400, gin.H{"error": "invalid cursor: likes_count_before must be >= 0"})
			return
		}

		// 校验：ID 必须大于 0（除非点赞数也是 0）
		if idBefore == 0 {
			if likesCountBefore != 0 {
				c.JSON(400, gin.H{"error": "invalid cursor: id_before must be > 0"})
				return
			}
		} else {
			// 构建复合游标
			cursor = &LikesCountCursor{
				LikesCount: likesCountBefore,
				ID:         idBefore,
			}
		}
	}

	// 4. 获取当前用户 ID（用于查询点赞状态）
	viewerAccountID, err := jwt.GetAccountID(c)
	if err != nil {
		viewerAccountID = 0
	}

	// 5. 调用 Service 层查询视频
	feedItems, err := f.service.ListLikesCount(c.Request.Context(), req.Limit, cursor, viewerAccountID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回响应
	c.JSON(200, feedItems)
}

// ============ 关注列表接口 ============

// ListByFollowing 查询用户关注的作者的视频（需要登录）
//
// 路由：POST /feed/listByFollowing
// 功能：按创建时间降序返回用户关注的作者的视频
// 场景：用户查看"关注"标签页，只看关注的作者发布的视频
//
// 请求示例：
//   {
//     "limit": 10,
//     "latest_time": 1640000000  // 游标：上一页最后一条视频的时间
//   }
//
// 响应示例：
//   {
//     "video_list": [...],
//     "next_time": 1639999500,
//     "has_more": true
//   }
//
// 注意：
//   - 需要登录（JWT 认证）
//   - 未登录用户返回 401 错误
//   - 如果用户没有关注任何人，返回空列表
//
// 参数：
//   c - Gin 上下文
func (f *FeedHandler) ListByFollowing(c *gin.Context) {
	// 1. 解析请求参数
	var req ListByFollowingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验并限制 limit
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 10 // 默认值
	}

	// 3. 获取当前用户 ID（必须登录）
	viewerAccountID, err := jwt.GetAccountID(c)
	if err != nil {
		// 未登录，返回 401
		c.JSON(401, gin.H{"error": "unauthorized"})
		return
	}

	// 4. 转换游标时间戳
	var latestTime time.Time
	if req.LatestTime > 0 {
		latestTime = time.Unix(req.LatestTime, 0)
	}

	// 5. 调用 Service 层查询视频
	feedItems, err := f.service.ListByFollowing(c.Request.Context(), req.Limit, latestTime, viewerAccountID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回响应
	c.JSON(200, feedItems)
}

// ============ 热门视频接口 ============

// ListByPopularity 按热度查询视频（公开接口，不需要登录）
//
// 路由：POST /feed/listByPopularity
// 功能：按热度降序返回热门视频（使用 Redis 热榜）
// 场景：用户查看"热门"标签页，看最火的内容
//
// 请求示例（第一页）：
//   {
//     "limit": 10,
//     "as_of": 0,    // 0 表示使用当前时间
//     "offset": 0     // 0 表示第一页
//   }
//
// 请求示例（第二页）：
//   {
//     "limit": 10,
//     "as_of": 1640000000,  // 使用第一页返回的 as_of（保持同一快照）
//     "offset": 10            // 从第 10 条开始
//   }
//
// 响应示例：
//   {
//     "video_list": [...],
//     "as_of": 1640000000,
//     "next_offset": 10,
//     "has_more": true,
//     "next_latest_popularity": 1500,
//     "next_latest_before": "2024-01-01T00:00:00Z",
//     "next_latest_id_before": 123
//   }
//
// 热榜设计说明：
//   - 使用 Redis 存储实时热度（ZSET 有序集合）
//   - 生成热榜快照（按分钟聚合）
//   - 使用 offset 分页（避免数据跳动）
//   - Redis 不可用时降级到数据库查询
//
// 参数：
//   c - Gin 上下文
func (f *FeedHandler) ListByPopularity(c *gin.Context) {
	// 1. 解析请求参数
	var req ListByPopularityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 校验并限制 limit
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = 10 // 默认值
	}

	// 3. 获取当前用户 ID（用于查询点赞状态，可选）
	viewerAccountID, err := jwt.GetAccountID(c)
	if err != nil {
		viewerAccountID = 0
	}

	// 4. 解析 DB Fallback 用游标（Redis 不可用时使用）
	var latestPopularity int64
	var latestBefore time.Time
	var latestIDBefore uint

	// 校验：热度不能为负数
	if req.LatestPopularity < 0 {
		c.JSON(400, gin.H{"error": "latest_popularity must be >= 0"})
		return
	}

	// 检查是否提供了游标（DB Fallback 用）
	anyCursor := !req.LatestBefore.IsZero() || req.LatestIDBefore != nil
	if anyCursor {
		// 校验：两个字段必须同时提供
		if req.LatestBefore.IsZero() || req.LatestIDBefore == nil || *req.LatestIDBefore == 0 {
			c.JSON(400, gin.H{"error": "latest_before and latest_id_before must be provided together"})
			return
		}
		// 解析游标
		latestPopularity = req.LatestPopularity
		latestBefore = req.LatestBefore
		latestIDBefore = *req.LatestIDBefore
	}

	// 5. 调用 Service 层查询视频
	resp, err := f.service.ListByPopularity(
		c.Request.Context(),
		req.Limit,
		req.AsOf,
		req.Offset,
		viewerAccountID,
		latestPopularity, // DB Fallback 用游标
		latestBefore,     // DB Fallback 用游标
		latestIDBefore,    // DB Fallback 用游标
	)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回响应
	c.JSON(200, resp)
}
