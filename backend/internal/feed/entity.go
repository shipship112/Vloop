// Package feed 定义了 Feed 流模块的数据模型和请求/响应结构
// Feed 流是视频推荐系统的核心，提供多种推荐算法
package feed

import "time"

// FeedAuthor 视频作者信息
type FeedAuthor struct {
	ID       uint   `json:"id"`       // 作者 ID
	Username string `json:"username"` // 作者用户名
}

// FeedVideoItem Feed 流中的视频项
// 包含视频基本信息、作者信息、点赞状态等
type FeedVideoItem struct {
	ID          uint       `json:"id"`           // 视频 ID
	Author      FeedAuthor `json:"author"`       // 作者信息
	Title       string     `json:"title"`        // 视频标题
	Description string     `json:"description"`  // 视频描述（可选）
	PlayURL     string     `json:"play_url"`     // 视频播放地址
	CoverURL    string     `json:"cover_url"`    // 视频封面地址
	CreateTime  int64      `json:"create_time"`  // 创建时间（Unix 时间戳）
	LikesCount  int64      `json:"likes_count"`  // 点赞数
	IsLiked     bool       `json:"is_liked"`    // 当前用户是否已点赞
}

// ============ 最新视频 Feed ============

// ListLatestRequest 查询最新视频的请求
type ListLatestRequest struct {
	Limit      int   `json:"limit"`       // 返回的视频数量（1-50）
	LatestTime int64 `json:"latest_time"` // 游标：上一页最后一条视频的创建时间（第一页传 0）
}

// ListLatestResponse 查询最新视频的响应
type ListLatestResponse struct {
	VideoList []FeedVideoItem `json:"video_list"` // 视频列表
	NextTime  int64           `json:"next_time"`  // 游标：用于下一页的时间戳
	HasMore   bool            `json:"has_more"`   // 是否还有更多数据
}

// ============ 点赞排行 Feed ============

// ListLikesCountRequest 按点赞数查询视频的请求
type ListLikesCountRequest struct {
	Limit            int    `json:"limit"`                  // 返回的视频数量（1-50）
	LikesCountBefore *int64 `json:"likes_count_before"` // 游标：上一页最后一条视频的点赞数（可选）
	IDBefore         *uint  `json:"id_before"`           // 游标：上一页最后一条视频的 ID（可选）
	// 注意：LikesCountBefore 和 IDBefore 必须同时提供或同时为空（复合游标）
}

// LikesCountCursor 点赞数游标（内部使用）
// 使用复合游标（点赞数 + ID）解决点赞数相同的情况
type LikesCountCursor struct {
	LikesCount int64 // 上一页最后一条视频的点赞数
	ID         uint  // 上一页最后一条视频的 ID
}

// ListLikesCountResponse 按点赞数查询视频的响应
type ListLikesCountResponse struct {
	VideoList            []FeedVideoItem `json:"video_list"`               // 视频列表
	NextLikesCountBefore *int64          `json:"next_likes_count_before"` // 游标：用于下一页的点赞数
	NextIDBefore         *uint           `json:"next_id_before"`          // 游标：用于下一页的 ID
	HasMore              bool            `json:"has_more"`                // 是否还有更多数据
}

// ============ 关注列表 Feed ============

// ListByFollowingRequest 查询关注列表视频的请求（需要登录）
type ListByFollowingRequest struct {
	Limit      int   `json:"limit"`       // 返回的视频数量（1-50）
	LatestTime int64 `json:"latest_time"` // 游标：上一页最后一条视频的创建时间（第一页传 0）
}

// ListByFollowingResponse 查询关注列表视频的响应
type ListByFollowingResponse struct {
	VideoList []FeedVideoItem `json:"video_list"` // 视频列表
	NextTime  int64           `json:"next_time"`  // 游标：用于下一页的时间戳
	HasMore   bool            `json:"has_more"`   // 是否还有更多数据
}

// ============ 热门视频 Feed ============

// ListByPopularityRequest 按热度查询视频的请求
type ListByPopularityRequest struct {
	Limit          int   `json:"limit"`                   // 返回的视频数量（1-50）
	AsOf           int64 `json:"as_of"`                 // 热榜快照时间（服务器返回的分钟时间戳，第一页传 0）
	Offset         int   `json:"offset"`                 // 分页偏移量（第一页传 0）
	LatestIDBefore *uint `json:"latest_id_before,omitempty"` // DB fallback 用：游标 ID

	// DB fallback 用（可选）：当 Redis 热榜不可用时，降级到数据库查询
	LatestPopularity int64     `json:"latest_popularity"` // 游标：上一页最后一条视频的热度
	LatestBefore     time.Time `json:"latest_before"`     // 游标：上一页最后一条视频的创建时间
}

// ListByPopularityResponse 按热度查询视频的响应
type ListByPopularityResponse struct {
	VideoList  []FeedVideoItem `json:"video_list"`                 // 视频列表
	AsOf       int64           `json:"as_of"`                     // 热榜快照时间（用于下一页）
	NextOffset int             `json:"next_offset"`               // 下一页的偏移量
	HasMore    bool            `json:"has_more"`                  // 是否还有更多数据

	// DB fallback 用：当 Redis 热榜不可用时，返回这些游标
	NextLatestPopularity *int64     `json:"next_latest_popularity,omitempty"` // 游标：用于下一页的热度
	NextLatestBefore     *time.Time `json:"next_latest_before,omitempty"`     // 游标：用于下一页的时间
	NextLatestIDBefore   *uint      `json:"next_latest_id_before,omitempty"`   // 游标：用于下一页的 ID
}
