package video

import (
	"context"
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// LikeRepository 点赞仓储层，负责点赞相关数据库操作
type LikeRepository struct {
	db *gorm.DB // GORM数据库实例
}

// NewLikeRepository 创建点赞仓储实例
func NewLikeRepository(db *gorm.DB) *LikeRepository {
	return &LikeRepository{db: db}
}

// Like 添加点赞记录
// 参数：
//   - ctx: 上下文
//   - like: 点赞对象
func (r *LikeRepository) Like(ctx context.Context, like *Like) error {
	return r.db.WithContext(ctx).Create(like).Error
}

// Unlike 删除点赞记录
// 参数：
//   - ctx: 上下文
//   - like: 点赞对象（包含videoID和accountID）
func (r *LikeRepository) Unlike(ctx context.Context, like *Like) error {
	return r.db.WithContext(ctx).
		Where("video_id = ? AND account_id = ?", like.VideoID, like.AccountID).
		Delete(&Like{}).Error
}

// LikeIgnoreDuplicate 添加点赞记录（忽略重复错误）
// 如果已点赞则返回created=false，否则返回created=true
// 参数：
//   - ctx: 上下文
//   - like: 点赞对象
// 返回：
//   - bool: 是否创建了新记录
//   - error: 错误信息
func (r *LikeRepository) LikeIgnoreDuplicate(ctx context.Context, like *Like) (created bool, err error) {
	if like == nil || like.VideoID == 0 || like.AccountID == 0 {
		return false, nil
	}
	err = r.db.WithContext(ctx).Create(like).Error
	if err == nil {
		return true, nil
	}
	// 唯一索引冲突（重复点赞）不算错误
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return false, nil
	}
	return false, err
}

// DeleteByVideoAndAccount 根据视频ID和用户ID删除点赞记录
// 参数：
//   - ctx: 上下文
//   - videoID: 视频ID
//   - accountID: 用户ID
// 返回：
//   - bool: 是否删除成功
//   - error: 错误信息
func (r *LikeRepository) DeleteByVideoAndAccount(ctx context.Context, videoID, accountID uint) (deleted bool, err error) {
	if videoID == 0 || accountID == 0 {
		return false, nil
	}
	res := r.db.WithContext(ctx).
		Where("video_id = ? AND account_id = ?", videoID, accountID).
		Delete(&Like{})
	return res.RowsAffected > 0, res.Error
}

// IsLiked 查询是否已点赞
// 参数：
//   - ctx: 上下文
//   - videoID: 视频ID
//   - accountID: 用户ID
// 返回：
//   - bool: 是否已点赞
//   - error: 错误信息
func (r *LikeRepository) IsLiked(ctx context.Context, videoID, accountID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&Like{}).
		Where("video_id = ? AND account_id = ?", videoID, accountID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// BatchGetLiked 批量查询是否已点赞（用于Feed流场景）
// 参数：
//   - ctx: 上下文
//   - videoIDs: 视频ID列表
//   - accountID: 用户ID
// 返回：
//   - map[uint]bool: videoID -> 是否已点赞
//   - error: 错误信息
func (r *LikeRepository) BatchGetLiked(ctx context.Context, videoIDs []uint, accountID uint) (map[uint]bool, error) {
	likeMap := make(map[uint]bool)
	if len(videoIDs) == 0 {
		return likeMap, nil
	}
	if accountID == 0 {
		return likeMap, nil
	}
	var likes []Like
	err := r.db.WithContext(ctx).Model(&Like{}).
		Where("video_id IN ? AND account_id = ?", videoIDs, accountID).
		Find(&likes).Error
	if err != nil {
		return nil, err
	}
	for _, like := range likes {
		likeMap[like.VideoID] = true
	}
	return likeMap, nil
}

// ListLikedVideos 查询用户点赞的视频列表
// 使用JOIN查询，按点赞时间倒序排列
// 参数：
//   - ctx: 上下文
//   - accountID: 用户ID
// 返回：
//   - []Video: 视频列表
//   - error: 错误信息
func (r *LikeRepository) ListLikedVideos(ctx context.Context, accountID uint) ([]Video, error) {
	var videos []Video
	if accountID == 0 {
		return videos, nil
	}
	err := r.db.WithContext(ctx).
		Model(&Video{}).
		Joins("JOIN likes ON likes.video_id = videos.id").
		Where("likes.account_id = ?", accountID).
		Order("likes.created_at desc").
		Find(&videos).Error
	if err != nil {
		return nil, err
	}
	return videos, nil
}
