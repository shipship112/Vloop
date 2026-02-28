package video

import "time"

// Video 视频实体模型，对应数据库中的videos表
type Video struct {
	ID          uint      `gorm:"primaryKey" json:"id"`                     // 主键ID
	AuthorID    uint      `gorm:"index;not null" json:"author_id"`          // 作者ID（带索引）
	Username    string    `gorm:"type:varchar(255);not null" json:"username"` // 作者用户名（冗余存储，便于查询）
	Title       string    `gorm:"type:varchar(255);not null" json:"title"`  // 视频标题
	Description string    `gorm:"type:varchar(255);" json:"description,omitempty"` // 视频描述（可选）
	PlayURL     string    `gorm:"type:varchar(255);not null" json:"play_url"` // 播放地址
	CoverURL    string    `gorm:"type:varchar(255);not null" json:"cover_url"` // 封面地址
	CreateTime  time.Time `gorm:"autoCreateTime" json:"create_time"`        // 创建时间（自动生成）
	LikesCount  int64     `gorm:"column:likes_count;not null;default:0" json:"likes_count"` // 点赞数
	Popularity  int64     `gorm:"column:popularity;not null;default:0" json:"popularity"` // 热度值
}

// PublishVideoRequest 发布视频请求体
type PublishVideoRequest struct {
	Title       string `json:"title"`       // 视频标题
	Description string `json:"description"` // 视频描述
	PlayURL     string `json:"play_url"`    // 播放地址
	CoverURL    string `json:"cover_url"`   // 封面地址
}

// DeleteVideoRequest 删除视频请求体
type DeleteVideoRequest struct {
	ID uint `json:"id"` // 视频ID
}

// ListByAuthorIDRequest 查询作者视频列表请求体
type ListByAuthorIDRequest struct {
	AuthorID uint `json:"author_id"` // 作者ID
}

// GetDetailRequest 获取视频详情请求体
type GetDetailRequest struct {
	ID uint `json:"id"` // 视频ID
}

// UpdateLikesCountRequest 更新点赞数请求体
type UpdateLikesCountRequest struct {
	ID         uint  `json:"id"`          // 视频ID
	LikesCount int64 `json:"likes_count"` // 新的点赞数
}
