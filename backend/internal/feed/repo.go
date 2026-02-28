// Package feed 定义了 Feed 流的数据访问层
// 职责：执行数据库查询，返回视频数据
package feed

import (
	"context"
	"feedsystem_video_go/internal/social"
	"feedsystem_video_go/internal/video"
	"time"

	"gorm.io/gorm"
)

// FeedRepository Feed 流仓储
type FeedRepository struct {
	db *gorm.DB // GORM 数据库连接
}

// NewFeedRepository 创建 Feed 仓储实例
func NewFeedRepository(db *gorm.DB) *FeedRepository {
	return &FeedRepository{db: db}
}

// ============ 查询最新视频 ============

// ListLatest 按创建时间降序查询最新视频（游标分页）
// 使用游标分页避免数据重复和遗漏
//
// SQL 等价查询：
//   SELECT * FROM videos
//   WHERE create_time < ?
//   ORDER BY create_time DESC
//   LIMIT ?;
//
// 参数：
//   ctx - 上下文
//   limit - 返回的视频数量
//   latestBefore - 游标：上一页最后一条视频的创建时间（零值表示第一页）
//
// 返回：
//   []*video.Video - 视频列表
//   error - 错误信息
func (repo *FeedRepository) ListLatest(ctx context.Context, limit int, latestBefore time.Time) ([]*video.Video, error) {
	var videos []*video.Video

	// 构建查询：按创建时间降序
	query := repo.db.WithContext(ctx).Model(&video.Video{}).
		Order("create_time DESC")

	// 游标分页：只查询小于游标时间的数据
	if !latestBefore.IsZero() {
		query = query.Where("create_time < ?", latestBefore)
	}

	// 执行查询
	if err := query.Limit(limit).Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

// ============ 按点赞数查询视频（复合游标） ============

// ListLikesCountWithCursor 按点赞数降序查询视频（复合游标分页）
// 使用复合游标（点赞数 + ID）解决点赞数相同的情况
//
// SQL 等价查询：
//   SELECT * FROM videos
//   WHERE
//     (likes_count < ?) OR
//     (likes_count = ? AND id < ?)
//   ORDER BY likes_count DESC, id DESC
//   LIMIT ?;
//
// 复合游标原理：
//   当多个视频点赞数相同时，使用 ID 作为第二排序字段
//   确保分页时数据不重复、不遗漏
//
// 参数：
//   ctx - 上下文
//   limit - 返回的视频数量
//   cursor - 复合游标（点赞数 + ID），nil 表示第一页
//
// 返回：
//   []*video.Video - 视频列表
//   error - 错误信息
func (repo *FeedRepository) ListLikesCountWithCursor(ctx context.Context, limit int, cursor *LikesCountCursor) ([]*video.Video, error) {
	var videos []*video.Video

	// 构建查询：先按点赞数降序，再按 ID 降序
	query := repo.db.WithContext(ctx).Model(&video.Video{}).
		Order("likes_count DESC, id DESC")

	// 复合游标：点赞数 + ID
	// 条件说明：
	//   1. likes_count < cursor.LikesCount：点赞数小于游标值
	//   2. likes_count = cursor.LikesCount AND id < cursor.ID：点赞数相等但 ID 小于游标值
	if cursor != nil {
		query = query.Where(
			"(likes_count < ?) OR (likes_count = ? AND id < ?)",
			cursor.LikesCount,              // 点赞数小于游标值
			cursor.LikesCount, cursor.ID,  // 点赞数相等但 ID 小于游标值
		)
	}

	// 执行查询
	if err := query.Limit(limit).Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

// ============ 查询关注列表视频 ============

// ListByFollowing 查询用户关注的作者的视频（游标分页）
// 使用子查询获取用户关注的作者 ID 列表
//
// SQL 等价查询：
//   SELECT * FROM videos
//   WHERE author_id IN (
//     SELECT vlogger_id FROM socials
//     WHERE follower_id = ?
//   )
//   ORDER BY create_time DESC
//   LIMIT ?;
//
// 参数：
//   ctx - 上下文
//   limit - 返回的视频数量
//   viewerAccountID - 当前用户的 ID（0 表示未登录，返回空列表）
//   latestBefore - 游标：上一页最后一条视频的创建时间（零值表示第一页）
//
// 返回：
//   []*video.Video - 视频列表
//   error - 错误信息
func (repo *FeedRepository) ListByFollowing(ctx context.Context, limit int, viewerAccountID uint, latestBefore time.Time) ([]*video.Video, error) {
	var videos []*video.Video

	// 构建查询：按创建时间降序
	query := repo.db.WithContext(ctx).Model(&video.Video{}).
		Order("create_time DESC")

	// 使用子查询：只查询用户关注的作者的视频
	if viewerAccountID > 0 {
		// 子查询：获取用户关注的所有作者 ID
		followingSubQuery := repo.db.WithContext(ctx).
			Model(&social.Social{}).
			Select("vlogger_id").                 // 查询作者 ID
			Where("follower_id = ?", viewerAccountID) // 当前用户关注的

		// 主查询：只查询这些作者的视频
		query = query.Where("author_id IN (?)", followingSubQuery)
	}

	// 游标分页：只查询小于游标时间的数据
	if !latestBefore.IsZero() {
		query = query.Where("create_time < ?", latestBefore)
	}

	// 执行查询
	if err := query.Limit(limit).Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

// ============ 按热度查询视频（DB Fallback） ============

// ListByPopularity 按热度降序查询视频（DB Fallback 方式）
// 当 Redis 热榜不可用时，降级到数据库查询
//
// SQL 等价查询：
//   SELECT * FROM videos
//   WHERE
//     (popularity < ?) OR
//     (popularity = ? AND create_time < ?) OR
//     (popularity = ? AND create_time = ? AND id < ?)
//   ORDER BY popularity DESC, create_time DESC, id DESC
//   LIMIT ?;
//
// 三重复合游标（热度 + 时间 + ID）：
//   当多个视频热度相同时，使用时间作为第二排序
//   当热度、时间都相同时，使用 ID 作为第三排序
//
// 参数：
//   ctx - 上下文
//   limit - 返回的视频数量
//   popularityBefore - 游标：上一页最后一条视频的热度
//   timeBefore - 游标：上一页最后一条视频的创建时间
//   idBefore - 游标：上一页最后一条视频的 ID
//
// 返回：
//   []*video.Video - 视频列表
//   error - 错误信息
func (repo *FeedRepository) ListByPopularity(ctx context.Context, limit int, popularityBefore int64, timeBefore time.Time, idBefore uint) ([]*video.Video, error) {
	var videos []*video.Video

	// 构建查询：先按热度降序，再按时间降序，最后按 ID 降序
	query := repo.db.WithContext(ctx).Model(&video.Video{}).
		Order("popularity DESC, create_time DESC, id DESC")

	// 三重复合游标：热度 + 时间 + ID
	// 只有当游标完整提供时才加过滤（popularity 允许为 0）
	if !timeBefore.IsZero() && idBefore > 0 {
		query = query.Where(
			"(popularity < ?) OR "+
			"(popularity = ? AND create_time < ?) OR "+
			"(popularity = ? AND create_time = ? AND id < ?)",
			popularityBefore,                       // 热度小于游标值
			popularityBefore, timeBefore,           // 热度相等但时间小于游标值
			popularityBefore, timeBefore, idBefore, // 热度、时间都相等但 ID 小于游标值
		)
	}

	// 执行查询
	if err := query.Limit(limit).Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}

// ============ 批量查询视频（按 ID） ============

// GetByIDs 根据视频 ID 列表批量查询视频
// 用于 Redis 热榜：先从 Redis 获取视频 ID，再从数据库查询详细信息
//
// SQL 等价查询：
//   SELECT * FROM videos
//   WHERE id IN (?, ?, ?, ...)
//   ORDER BY FIELD(id, ?, ?, ?, ...)  -- 保持传入顺序
//
// 注意：本方法只负责查询，排序由 Service 层处理
//
// 参数：
//   ctx - 上下文
//   ids - 视频 ID 列表
//
// 返回：
//   []*video.Video - 视频列表
//   error - 错误信息
func (repo *FeedRepository) GetByIDs(ctx context.Context, ids []uint) ([]*video.Video, error) {
	var videos []*video.Video

	// 空列表直接返回
	if len(ids) == 0 {
		return videos, nil
	}

	// 批量查询
	if err := repo.db.WithContext(ctx).Model(&video.Video{}).
		Where("id IN ?", ids).Find(&videos).Error; err != nil {
		return nil, err
	}
	return videos, nil
}
