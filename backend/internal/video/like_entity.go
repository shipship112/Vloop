package video

import "time"

// Like 点赞实体模型，对应数据库中的likes表
// 使用联合唯一索引 (video_id, account_id) 防止重复点赞
type Like struct {
	ID        uint      `gorm:"primaryKey" json:"id"`                                  // 主键ID
	VideoID   uint      `gorm:"uniqueIndex:idx_like_video_account;not null" json:"video_id"`   // 视频ID（联合唯一索引）
	AccountID uint      `gorm:"uniqueIndex:idx_like_video_account;not null" json:"account_id"` // 用户ID（联合唯一索引）
	CreatedAt time.Time `json:"created_at"`                                                // 点赞时间
}

// LikeRequest 点赞请求体
type LikeRequest struct {
	VideoID uint `json:"video_id"` // 视频ID
}
