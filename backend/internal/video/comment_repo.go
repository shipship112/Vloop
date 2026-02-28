package video

import (
	"context"

	"gorm.io/gorm"
)

// CommentRepository 评论仓储层，负责评论相关数据库操作
type CommentRepository struct {
	db *gorm.DB // GORM数据库实例
}

// NewCommentRepository 创建评论仓储实例
func NewCommentRepository(db *gorm.DB) *CommentRepository {
	return &CommentRepository{db: db}
}

// CreateComment 添加评论记录
// 参数：
//   - ctx: 上下文
//   - comment: 评论对象
func (r *CommentRepository) CreateComment(ctx context.Context, comment *Comment) error {
	return r.db.WithContext(ctx).Create(comment).Error
}

// DeleteComment 删除评论记录
// 参数：
//   - ctx: 上下文
//   - comment: 评论对象
func (r *CommentRepository) DeleteComment(ctx context.Context, comment *Comment) error {
	return r.db.WithContext(ctx).Delete(comment).Error
}

// GetAllComments 查询指定视频的所有评论
// 按创建时间倒序排列
// 参数：
//   - ctx: 上下文
//   - videoID: 视频ID
// 返回：
//   - []Comment: 评论列表
//   - error: 错误信息
func (r *CommentRepository) GetAllComments(ctx context.Context, videoID uint) ([]Comment, error) {
	var comments []Comment
	err := r.db.WithContext(ctx).Where("video_id = ?", videoID).Find(&comments).Error
	return comments, err
}

// IsExist 检查评论是否存在
// 参数：
//   - ctx: 上下文
//   - id: 评论ID
// 返回：
//   - bool: 是否存在
//   - error: 错误信息
func (r *CommentRepository) IsExist(ctx context.Context, id uint) (bool, error) {
	var comment Comment
	if err := r.db.WithContext(ctx).First(&comment, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetByID 根据ID查询评论详情
// 参数：
//   - ctx: 上下文
//   - id: 评论ID
// 返回：
//   - *Comment: 评论对象
//   - error: 错误信息
func (r *CommentRepository) GetByID(ctx context.Context, id uint) (*Comment, error) {
	var comment Comment
	if err := r.db.WithContext(ctx).First(&comment, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &comment, nil
}
