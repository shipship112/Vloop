package video

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// VideoRepository 视频仓储层，负责视频数据库操作
type VideoRepository struct {
	db *gorm.DB // GORM数据库实例
}

// NewVideoRepository 创建视频仓储实例
func NewVideoRepository(db *gorm.DB) *VideoRepository {
	return &VideoRepository{db: db}
}

// CreateVideo 创建视频记录
// 参数：
//   - ctx: 上下文
//   - video: 视频对象
func (vr *VideoRepository) CreateVideo(ctx context.Context, video *Video) error {
	if err := vr.db.WithContext(ctx).Create(video).Error; err != nil {
		return err
	}
	return nil
}

// DeleteVideo 删除视频记录
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
func (vr *VideoRepository) DeleteVideo(ctx context.Context, id uint) error {
	if err := vr.db.WithContext(ctx).Delete(&Video{}, id).Error; err != nil {
		return err
	}
	return nil
}

// ListByAuthorID 查询指定作者的视频列表
// 返回按创建时间倒序排列的视频列表
// 参数：
//   - ctx: 上下文
//   - authorID: 作者ID
// 返回：
//   - []Video: 视频列表
//   - error: 错误信息
func (vr *VideoRepository) ListByAuthorID(ctx context.Context, authorID int64) ([]Video, error) {
	var videos []Video
	if err := vr.db.WithContext(ctx).
		Where("author_id = ?", authorID).
		Order("create_time desc").
		Offset(0).
		Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

// GetByID 根据ID查询视频详情
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
// 返回：
//   - *Video: 视频对象
//   - error: 错误信息
func (vr *VideoRepository) GetByID(ctx context.Context, id uint) (*Video, error) {
	var video Video
	if err := vr.db.WithContext(ctx).First(&video, id).Error; err != nil {
		return (*Video)(nil), err
	}
	return &video, nil
}

// UpdateLikesCount 更新视频点赞数（直接设置为指定值）
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
//   - likesCount: 新的点赞数
func (vr *VideoRepository) UpdateLikesCount(ctx context.Context, id uint, likesCount int64) error {
	if err := vr.db.WithContext(ctx).Model(&Video{}).
		Where("id = ?", id).
		Update("likes_count", likesCount).Error; err != nil {
		return err
	}
	return nil
}

// IsExist 检查视频是否存在
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
// 返回：
//   - bool: 是否存在
//   - error: 错误信息
func (vr *VideoRepository) IsExist(ctx context.Context, id uint) (bool, error) {
	var video Video
	if err := vr.db.WithContext(ctx).First(&video, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// UpdatePopularity 更新视频热度（增量更新）
// 使用SQL表达式：popularity = popularity + change
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
//   - change: 热度变化量（可为正数或负数）
func (vr *VideoRepository) UpdatePopularity(ctx context.Context, id uint, change int64) error {
	if err := vr.db.WithContext(ctx).Model(&Video{}).
		Where("id = ?", id).
		Update("popularity", gorm.Expr("popularity + ?", change)).Error; err != nil {
		return err
	}
	return nil
}

// ChangeLikesCount 增量更新点赞数（确保不小于0）
// 使用SQL表达式：likes_count = GREATEST(likes_count + change, 0)
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
//   - change: 点赞数变化量（可为正数或负数）
func (vr *VideoRepository) ChangeLikesCount(ctx context.Context, id uint, change int64) error {
	if err := vr.db.WithContext(ctx).Model(&Video{}).
		Where("id = ?", id).
		UpdateColumn("likes_count", gorm.Expr("GREATEST(likes_count + ?, 0)", change)).Error; err != nil {
		return err
	}
	return nil
}

// ChangePopularity 增量更新热度（确保不小于0）
// 使用SQL表达式：popularity = GREATEST(popularity + change, 0)
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
//   - change: 热度变化量（可为正数或负数）
func (vr *VideoRepository) ChangePopularity(ctx context.Context, id uint, change int64) error {
	if err := vr.db.WithContext(ctx).Model(&Video{}).
		Where("id = ?", id).
		UpdateColumn("popularity", gorm.Expr("GREATEST(popularity + ?, 0)", change)).Error; err != nil {
		return err
	}
	return nil
}
