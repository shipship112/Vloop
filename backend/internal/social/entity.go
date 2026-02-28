package social

import "feedsystem_video_go/internal/account"

// Social 关注关系实体模型，对应数据库中的socials表
// 使用联合唯一索引 (follower_id, vlogger_id) 防止重复关注
type Social struct {
	ID         uint `gorm:"primaryKey"`                                  // 主键ID
	FollowerID uint `gorm:"not null;index:idx_social_follower;uniqueIndex:idx_social_follower_vlogger"` // 关注者ID（带索引，联合唯一索引）
	VloggerID  uint `gorm:"not null;index:idx_social_vlogger;uniqueIndex:idx_social_follower_vlogger"`  // 被关注者（博主）ID（带索引，联合唯一索引）
}

// FollowRequest 关注请求体
type FollowRequest struct {
	VloggerID uint `json:"vlogger_id"` // 博主ID
}

// UnfollowRequest 取消关注请求体
type UnfollowRequest struct {
	VloggerID uint `json:"vlogger_id"` // 博主ID
}

// GetAllFollowersRequest 查询粉丝列表请求体
type GetAllFollowersRequest struct {
	VloggerID uint `json:"vlogger_id"` // 博主ID（可选，不传则查询当前用户的粉丝）
}

// GetAllFollowersResponse 查询粉丝列表响应体
type GetAllFollowersResponse struct {
	Followers []*account.Account `json:"followers"` // 粉丝列表
}

// GetAllVloggersRequest 查询关注列表请求体
type GetAllVloggersRequest struct {
	FollowerID uint `json:"follower_id"` // 关注者ID（可选，不传则查询当前用户的关注列表）
}

// GetAllVloggersResponse 查询关注列表响应体
type GetAllVloggersResponse struct {
	Vloggers []*account.Account `json:"vloggers"` // 关注的博主列表
}
