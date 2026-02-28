package video

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"feedsystem_video_go/internal/middleware/rabbitmq"
	rediscache "feedsystem_video_go/internal/middleware/redis"
)

// VideoService 视频服务层，处理视频业务逻辑
// - 职责：业务规则、缓存管理、消息队列推送
type VideoService struct {
	repo         *VideoRepository              // 视频仓储层，负责数据库操作
	cache        *rediscache.Client            // Redis缓存客户端
	cacheTTL     time.Duration                 // 缓存过期时间（5分钟）
	popularityMQ *rabbitmq.PopularityMQ         // 热度消息队列，用于异步更新热度
}

// NewVideoService 创建视频服务实例
func NewVideoService(repo *VideoRepository, cache *rediscache.Client, popularityMQ *rabbitmq.PopularityMQ) *VideoService {
	return &VideoService{repo: repo, cache: cache, cacheTTL: 5 * time.Minute, popularityMQ: popularityMQ}
}

// Publish 发布视频
// 业务流程：
// 1. 校验视频对象不为空
// 2. 去除标题、播放URL、封面URL的首尾空格
// 3. 校验必填字段（标题、播放URL、封面URL）
// 4. 调用Repository层将视频存入数据库
// 参数：
//   - ctx: 上下文
//   - video: 视频对象（包含作者ID、用户名、标题、描述、播放URL、封面URL）
func (vs *VideoService) Publish(ctx context.Context, video *Video) error {
	// 1. 校验视频对象不为空
	if video == nil {
		return errors.New("video is nil")
	}

	// 2. 去除首尾空格
	video.Title = strings.TrimSpace(video.Title)
	video.PlayURL = strings.TrimSpace(video.PlayURL)
	video.CoverURL = strings.TrimSpace(video.CoverURL)

	// 3. 校验必填字段
	if video.Title == "" {
		return errors.New("title is required")
	}
	if video.PlayURL == "" {
		return errors.New("play url is required")
	}
	if video.CoverURL == "" {
		return errors.New("cover url is required")
	}

	// 4. 调用Repository层将视频存入数据库
	if err := vs.repo.CreateVideo(ctx, video); err != nil {
		return err
	}
	return nil
}

// Delete 删除视频
// 业务流程：
// 1. 查询视频是否存在
// 2. 校验操作者是否为视频作者（防止删除他人视频）
// 3. 调用Repository层删除视频
// 4. 删除Redis缓存中的视频详情
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
//   - authorID: 操作者的账户ID
func (vs *VideoService) Delete(ctx context.Context, id uint, authorID uint) error {
	// 1. 查询视频是否存在
	video, err := vs.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if video == nil {
		return errors.New("video not found")
	}

	// 2. 校验操作者是否为视频作者
	if video.AuthorID != authorID {
		return errors.New("unauthorized")
	}

	// 3. 调用Repository层删除视频
	if err := vs.repo.DeleteVideo(ctx, id); err != nil {
		return err
	}

	// 4. 删除Redis缓存中的视频详情
	if vs.cache != nil {
		cacheKey := fmt.Sprintf("video:detail:id=%d", id)
		_ = vs.cache.Del(context.Background(), cacheKey)
	}
	return nil
}

// ListByAuthorID 查询作者的视频列表
// 业务流程：
// 1. 调用Repository层查询指定作者的所有视频
// 2. 返回按创建时间倒序排列的视频列表
// 参数：
//   - ctx: 上下文
//   - authorID: 作者ID
// 返回：
//   - []Video: 视频列表（按创建时间倒序）
//   - error: 错误信息
func (vs *VideoService) ListByAuthorID(ctx context.Context, authorID uint) ([]Video, error) {
	// 调用Repository层查询指定作者的所有视频
	videos, err := vs.repo.ListByAuthorID(ctx, int64(authorID))
	if err != nil {
		return nil, err
	}
	return videos, nil
}

// GetDetail 获取视频详情（含缓存逻辑）
// 业务流程：
// 1. 尝试从Redis缓存读取视频详情
// 2. 如果缓存未命中，使用分布式锁防止缓存击穿
// 3. 拿到锁的请求从数据库查询并回填缓存
// 4. 没拿到锁的请求等待并重试读取缓存
// 5. 如果缓存禁用，直接查询数据库
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
// 返回：
//   - *Video: 视频详情
//   - error: 错误信息
func (vs *VideoService) GetDetail(ctx context.Context, id uint) (*Video, error) {
	// 缓存键格式：video:detail:id={视频ID}
	cacheKey := fmt.Sprintf("video:detail:id=%d", id)

	// 内部函数：从缓存获取视频
	getCached := func() (*Video, bool) {
		opCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		b, err := vs.cache.GetBytes(opCtx, cacheKey)
		if err != nil {
			return nil, false
		}
		var cached Video
		if err := json.Unmarshal(b, &cached); err != nil {
			return nil, false
		}
		return &cached, true
	}

	// 内部函数：将视频存入缓存
	setCached := func(video *Video) {
		b, err := json.Marshal(video)
		if err != nil {
			return
		}
		opCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		_ = vs.cache.SetBytes(opCtx, cacheKey, b, vs.cacheTTL)
	}

	// 如果启用了缓存
	if vs.cache != nil {
		// 1. 第一次尝试从缓存读取
		if v, ok := getCached(); ok {
			return v, nil
		}

		// 2. 再次尝试读取（可能已被其他请求回填）
		opCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		b, err := vs.cache.GetBytes(opCtx, cacheKey)
		cancel()
		if err == nil {
			var cached Video
			if err := json.Unmarshal(b, &cached); err == nil {
				return &cached, nil
			}
		} else if rediscache.IsMiss(err) {
			// 3. 缓存未命中，尝试获取分布式锁
			lockKey := "lock:" + cacheKey

			lockCtx, lockCancel := context.WithTimeout(ctx, 50*time.Millisecond)
			token, locked, lockErr := vs.cache.Lock(lockCtx, lockKey, 2*time.Second)
			lockCancel()

			if lockErr == nil && locked {
				// 4. 拿到锁：再次检查缓存（防止锁竞争）
				defer func() { _ = vs.cache.Unlock(context.Background(), lockKey, token) }()

				if v, ok := getCached(); ok {
					return v, nil
				}

				// 5. 从数据库查询视频
				video, err := vs.repo.GetByID(ctx, id)
				if err != nil {
					return nil, err
				}

				// 6. 回填缓存
				setCached(video)
				return video, nil
			}

			// 7. 没拿到锁：等待别人回填缓存（最多5次，每次间隔20ms）
			for i := 0; i < 5; i++ {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(20 * time.Millisecond):
				}
				if v, ok := getCached(); ok {
					return v, nil
				}
			}
		}
	}

	// 8. 缓存禁用或获取失败，直接查询数据库
	video, err := vs.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// 9. 回填缓存（如果启用）
	if vs.cache != nil {
		setCached(video)
	}
	return video, nil
}

// UpdateLikesCount 更新视频点赞数（直接设置为指定值）
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
//   - likesCount: 新的点赞数
func (vs *VideoService) UpdateLikesCount(ctx context.Context, id uint, likesCount int64) error {
	// 调用Repository层更新点赞数
	if err := vs.repo.UpdateLikesCount(ctx, id, likesCount); err != nil {
		return err
	}
	return nil
}

// UpdatePopularity 更新视频热度
// 业务流程：
// 1. 更新数据库中的热度值
// 2. 如果MQ可用，发送热度更新消息到队列（供Worker异步处理）
// 3. 删除Redis缓存中的视频详情
// 4. 将热度变化写入Redis时间窗有序集合（用于热榜统计）
// 参数：
//   - ctx: 上下文
//   - id: 视频ID
//   - change: 热度变化量（可为正数或负数）
func (vs *VideoService) UpdatePopularity(ctx context.Context, id uint, change int64) error {
	// 1. 更新数据库中的热度值
	if err := vs.repo.UpdatePopularity(ctx, id, change); err != nil {
		return err
	}

	// 2. 如果MQ可用，发送热度更新消息到队列（供Worker异步处理）
	if vs.popularityMQ != nil {
		if err := vs.popularityMQ.Update(ctx, id, change); err == nil {
			return nil
		}
	}

	// 3. 如果MQ不可用，直接操作Redis缓存
	if vs.cache != nil {
		// 3.1 删除Redis缓存中的视频详情（保证数据一致性）
		_ = vs.cache.Del(context.Background(), fmt.Sprintf("video:detail:id=%d", id))

		// 3.2 将热度变化写入Redis时间窗有序集合（用于热榜统计）
		// 时间窗格式：hot:video:1m:{YYYYMMDDHHMM}，每分钟一个窗口
		now := time.Now().UTC().Truncate(time.Minute)
		windowKey := "hot:video:1m:" + now.Format("200601021504")
		member := strconv.FormatUint(uint64(id), 10)

		opCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		// 增加视频在时间窗中的热度值
		_ = vs.cache.ZincrBy(opCtx, windowKey, member, float64(change))
		// 设置时间窗过期时间为2小时
		_ = vs.cache.Expire(opCtx, windowKey, 2*time.Hour)
	}
	return nil
}
