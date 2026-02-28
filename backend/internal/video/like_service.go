package video

import (
	"context"
	"errors"
	"feedsystem_video_go/internal/middleware/rabbitmq"
	rediscache "feedsystem_video_go/internal/middleware/redis"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// LikeService 点赞服务层，处理点赞业务逻辑
// - 支持MQ异步处理（推荐）
// - 支持Fallback降级（MQ失败时直接写数据库/Redis）
type LikeService struct {
	repo         *LikeRepository              // 点赞仓储层，负责数据库操作
	VideoRepo    *VideoRepository             // 视频仓储层，校验视频是否存在
	cache        *rediscache.Client            // Redis缓存客户端
	likeMQ       *rabbitmq.LikeMQ             // 点赞消息队列，异步处理点赞记录和点赞数
	popularityMQ *rabbitmq.PopularityMQ       // 热度消息队列，异步更新视频热度
}

// NewLikeService 创建点赞服务实例
func NewLikeService(repo *LikeRepository, videoRepo *VideoRepository, cache *rediscache.Client, likeMQ *rabbitmq.LikeMQ, popularityMQ *rabbitmq.PopularityMQ) *LikeService {
	return &LikeService{repo: repo, VideoRepo: videoRepo, cache: cache, likeMQ: likeMQ, popularityMQ: popularityMQ}
}

// isDupKey 判断错误是否为MySQL唯一索引冲突（错误码1062）
func isDupKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// Like 点赞视频
// 业务流程：
// 1. 校验参数（视频ID和用户ID）
// 2. 校验视频是否存在
// 3. 校验是否已点赞（防止重复点赞）
// 4. 优先使用MQ异步处理：发送点赞消息到队列
// 5. MQ失败时Fallback：直接写入数据库事务
// 6. MQ失败时Fallback：直接更新Redis热度缓存
// 参数：
//   - ctx: 上下文
//   - like: 点赞对象
func (s *LikeService) Like(ctx context.Context, like *Like) error {
	// 1. 校验参数
	if like == nil {
		return errors.New("like is nil")
	}
	if like.VideoID == 0 || like.AccountID == 0 {
		return errors.New("video_id and account_id are required")
	}

	// 2. 校验视频是否存在
	if s.VideoRepo != nil {
		ok, err := s.VideoRepo.IsExist(ctx, like.VideoID)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("video not found")
		}
	}

	// 3. 校验是否已点赞（防止重复点赞）
	isLiked, err := s.repo.IsLiked(ctx, like.VideoID, like.AccountID)
	if err != nil {
		return err
	}
	if isLiked {
		return errors.New("user has liked this video")
	}

	// 4. 设置点赞时间
	like.CreatedAt = time.Now()

	// 5. 尝试使用MQ异步处理
	mysqlEnqueued := false // 是否成功发送点赞MQ消息
	redisEnqueued := false // 是否成功发送热度MQ消息

	// 5.1 发送点赞消息到MQ（Worker异步处理点赞记录和点赞数）
	if s.likeMQ != nil {
		if err := s.likeMQ.Like(ctx, like.AccountID, like.VideoID); err == nil {
			mysqlEnqueued = true
		}
	}

	// 5.2 发送热度更新消息到MQ（Worker异步更新视频热度）
	if s.popularityMQ != nil {
		if err := s.popularityMQ.Update(ctx, like.VideoID, 1); err == nil {
			redisEnqueued = true
		}
	}

	// 5.3 如果两个MQ都成功发送，直接返回（Worker会异步处理）
	if mysqlEnqueued && redisEnqueued {
		return nil
	}

	// 6. Fallback: 点赞MQ发送失败时，直接写入数据库事务
	if !mysqlEnqueued {
		err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 6.1 再次校验视频是否存在（事务内）
			if err := tx.Select("id").First(&Video{}, like.VideoID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errors.New("video not found")
				}
				return err
			}

			// 6.2 插入点赞记录
			if err := tx.Create(like).Error; err != nil {
				if isDupKey(err) {
					return errors.New("user has liked this video")
				}
				return err
			}

			// 6.3 更新视频点赞数（增量+1）
			if err := tx.Model(&Video{}).Where("id = ?", like.VideoID).
				UpdateColumn("likes_count", gorm.Expr("likes_count + 1")).Error; err != nil {
				return err
			}

			// 6.4 更新视频热度（增量+1）
			return tx.Model(&Video{}).Where("id = ?", like.VideoID).
				UpdateColumn("popularity", gorm.Expr("popularity + 1")).Error
		})
		if err != nil {
			return err
		}
	}

	// 7. Fallback: 热度MQ发送失败时，直接更新Redis热度缓存
	if !redisEnqueued {
		UpdatePopularityCache(ctx, s.cache, like.VideoID, 1)
	}

	return nil
}

// Unlike 取消点赞
// 业务流程：
// 1. 校验参数（视频ID和用户ID）
// 2. 校验视频是否存在
// 3. 校验是否已点赞（防止取消未点赞的视频）
// 4. 优先使用MQ异步处理：发送取消点赞消息到队列
// 5. MQ失败时Fallback：直接写入数据库事务
// 6. MQ失败时Fallback：直接更新Redis热度缓存
// 参数：
//   - ctx: 上下文
//   - like: 点赞对象
func (s *LikeService) Unlike(ctx context.Context, like *Like) error {
	// 1. 校验参数
	if like == nil {
		return errors.New("like is nil")
	}
	if like.VideoID == 0 || like.AccountID == 0 {
		return errors.New("video_id and account_id are required")
	}

	// 2. 校验视频是否存在
	if s.VideoRepo != nil {
		ok, err := s.VideoRepo.IsExist(ctx, like.VideoID)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("video not found")
		}
	}

	// 3. 校验是否已点赞（防止取消未点赞的视频）
	isLiked, err := s.repo.IsLiked(ctx, like.VideoID, like.AccountID)
	if err != nil {
		return err
	}
	if !isLiked {
		return errors.New("user has not liked this video")
	}

	// 4. 尝试使用MQ异步处理
	mysqlEnqueued := false // 是否成功发送取消点赞MQ消息
	redisEnqueued := false // 是否成功发送热度更新MQ消息

	// 4.1 发送取消点赞消息到MQ（Worker异步处理）
	if s.likeMQ != nil {
		if err := s.likeMQ.Unlike(ctx, like.AccountID, like.VideoID); err == nil {
			mysqlEnqueued = true
		}
	}

	// 4.2 发送热度更新消息到MQ（热度-1）
	if s.popularityMQ != nil {
		if err := s.popularityMQ.Update(ctx, like.VideoID, -1); err == nil {
			redisEnqueued = true
		}
	}

	// 4.3 如果两个MQ都成功发送，直接返回
	if mysqlEnqueued && redisEnqueued {
		return nil
	}

	// 5. Fallback: 点赞MQ发送失败时，直接写入数据库事务
	if !mysqlEnqueued {
		err := s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 5.1 删除点赞记录
			del := tx.Where("video_id = ? AND account_id = ?", like.VideoID, like.AccountID).Delete(&Like{})
			if del.Error != nil {
				return del.Error
			}
			if del.RowsAffected == 0 {
				return errors.New("user has not liked this video")
			}

			// 5.2 更新视频点赞数（增量-1，确保不小于0）
			if err := tx.Model(&Video{}).Where("id = ?", like.VideoID).
				UpdateColumn("likes_count", gorm.Expr("GREATEST(likes_count - 1, 0)")).Error; err != nil {
				return err
			}

			// 5.3 更新视频热度（增量-1，确保不小于0）
			return tx.Model(&Video{}).Where("id = ?", like.VideoID).
				UpdateColumn("popularity", gorm.Expr("GREATEST(popularity - 1, 0)")).Error
		})
		if err != nil {
			return err
		}
	}

	// 6. Fallback: 热度MQ发送失败时，直接更新Redis热度缓存
	if !redisEnqueued {
		UpdatePopularityCache(ctx, s.cache, like.VideoID, -1)
	}

	return nil
}

// IsLiked 查询是否已点赞
// 参数：
//   - ctx: 上下文
//   - videoID: 视频ID
//   - accountID: 用户ID
// 返回：
//   - bool: 是否已点赞
//   - error: 错误信息
func (s *LikeService) IsLiked(ctx context.Context, videoID, accountID uint) (bool, error) {
	return s.repo.IsLiked(ctx, videoID, accountID)
}

// ListLikedVideos 查询用户点赞的视频列表
// 参数：
//   - ctx: 上下文
//   - accountID: 用户ID
// 返回：
//   - []Video: 视频列表（按点赞时间倒序）
//   - error: 错误信息
func (s *LikeService) ListLikedVideos(ctx context.Context, accountID uint) ([]Video, error) {
	return s.repo.ListLikedVideos(ctx, accountID)
}
