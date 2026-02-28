package video

import "time"

// Comment 评论实体模型，对应数据库中的comments表
type Comment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`                // 主键ID
	Username  string    `gorm:"index" json:"username"`              // 评论者用户名（冗余存储，便于查询）
	VideoID   uint      `gorm:"index" json:"video_id"`              // 视频ID（带索引，用于查询）
	AuthorID  uint      `gorm:"index" json:"author_id"`             // 评论者ID（带索引，用于查询）
	Content   string    `gorm:"type:text" json:"content"`           // 评论内容（TEXT类型，支持长文本）
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`   // 创建时间（自动生成）
}

// PublishCommentRequest 发布评论请求体
type PublishCommentRequest struct {
	VideoID uint   `json:"video_id"` // 视频ID
	Content string `json:"content"`  // 评论内容
}

// DeleteCommentRequest 删除评论请求体
type DeleteCommentRequest struct {
	CommentID uint `json:"comment_id"` // 评论ID
}

// GetAllCommentsRequest 查询评论列表请求体
type GetAllCommentsRequest struct {
	VideoID uint `json:"video_id"` // 视频ID
}
