// Package feed 定义了 Feed 流的业务逻辑层
// 职责：
//   1. 整合数据库查询和 Redis 缓存
//   2. 实现分布式锁防止缓存击穿
//   3. 批量查询点赞状态
//   4. 构建 FeedVideoItem 响应对象
package feed

import (
	"context"
	"encoding/json"
	rediscache "feedsystem_video_go/internal/middleware/redis"
	"feedsystem_video_go/internal/video"
	"fmt"
	"strconv"
	"time"
)

// FeedService Feed 流服务层
type FeedService struct {
	repo     *FeedRepository          // Feed 仓储（查询视频数据）
	likeRepo *video.LikeRepository   // 点赞仓储（查询点赞状态）
	cache    *rediscache.Client      // Redis 缓存客户端
	cacheTTL time.Duration          // 缓存过期时间
}

// NewFeedService 创建 Feed 服务实例
// 参数：
//   repo - Feed 仓储
//   likeRepo - 点赞仓储
//   cache - Redis 缓存客户端（可能为 nil）
// 返回：
//   *FeedService - Feed 服务实例
func NewFeedService(repo *FeedRepository, likeRepo *video.LikeRepository, cache *rediscache.Client) *FeedService {
	// 默认缓存过期时间：5 秒
	return &FeedService{
		repo:     repo,
		likeRepo: likeRepo,
		cache:    cache,
		cacheTTL: 5 * time.Second,
	}
}

// ============================================================================
// ============ 查询最新视频 ============
// ============================================================================

// ListLatest 查询最新视频（带缓存和分布式锁）
//
// 业务流程：
//   1. 尝试从 Redis 缓存读取
//   2. 缓存未命中 → 加分布式锁
//   3. 获取锁成功 → 再次检查缓存（防止重复查询）
//   4. 缓存仍然未命中 → 查询数据库
//   5. 写入缓存
//   6. 获取锁失败 → 短暂等待后重试（等待其他 goroutine 写入缓存）
//   7. 批量查询点赞状态
//   8. 构建响应并返回
//
// 缓存策略：
//   - 缓存键格式：feed:listLatest:limit=10:before=0
//   - 缓存过期时间：5 秒
//   - 仅对匿名用户缓存（viewerAccountID = 0）
//
// 分布式锁：
//   - 锁键格式：lock:feed:listLatest:limit=10:before=0
//   - 锁过期时间：500 毫秒
//   - 防止缓存击穿（大量并发同时查询数据库）
//
// 参数：
//   ctx - 上下文
//   limit - 返回的视频数量
//   latestBefore - 游标：上一页最后一条视频的创建时间
//   viewerAccountID - 当前用户 ID（0 表示匿名用户）
//
// 返回：
//   ListLatestResponse - 响应对象
//   error - 错误信息
func (f *FeedService) ListLatest(ctx context.Context, limit int, latestBefore time.Time, viewerAccountID uint) (ListLatestResponse, error) {
	// 定义数据库查询函数（闭包）
	// 职责：从数据库查询视频，构建响应对象
	doListLatestFromDB := func() (ListLatestResponse, error) {
		// 1. 从数据库查询视频
		videos, err := f.repo.ListLatest(ctx, limit, latestBefore)
		if err != nil {
			return ListLatestResponse{}, err
		}

		// 2. 计算下一页游标（最后一条视频的创建时间）
		var nextTime int64
		if len(videos) > 0 {
			nextTime = videos[len(videos)-1].CreateTime.Unix()
		} else {
			nextTime = 0
		}

		// 3. 判断是否还有更多数据
		hasMore := len(videos) == limit

		// 4. 批量查询点赞状态并构建 FeedVideoItem
		feedVideos, err := f.buildFeedVideos(ctx, videos, viewerAccountID)
		if err != nil {
			return ListLatestResponse{}, err
		}

		// 5. 构建响应对象
		resp := ListLatestResponse{
			VideoList: feedVideos,
			NextTime:  nextTime,
			HasMore:   hasMore,
		}
		return resp, nil
	}

	// ========== Redis 缓存逻辑 ==========

	// 缓存键格式：feed:listLatest:limit=10:before=0
	// 注意：仅对匿名用户缓存（viewerAccountID = 0）
	var cacheKey string
	if viewerAccountID == 0 && f.cache != nil {
		before := int64(0)
		if !latestBefore.IsZero() {
			before = latestBefore.Unix()
		}
		cacheKey = fmt.Sprintf("feed:listLatest:limit=%d:before=%d", limit, before)

		// 设置缓存查询超时：50 毫秒
		cacheCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		// 1. 尝试从 Redis 缓存读取
		b, err := f.cache.GetBytes(cacheCtx, cacheKey)
		if err == nil {
			// 缓存命中：反序列化并返回
			var cached ListLatestResponse
			if err := json.Unmarshal(b, &cached); err == nil {
				return cached, nil
			}
		} else if rediscache.IsMiss(err) { // 缓存未命中
			// 分布式锁键：lock:feed:listLatest:limit=10:before=0
			lockKey := "lock:" + cacheKey

			// 2. 尝试获取分布式锁（防止缓存击穿）
			token, locked, _ := f.cache.Lock(cacheCtx, lockKey, 500*time.Millisecond)
			if locked {
				// 获取锁成功：再次检查缓存（双重检查）
				defer func() { _ = f.cache.Unlock(context.Background(), lockKey, token) }()

				if b, err := f.cache.GetBytes(cacheCtx, cacheKey); err == nil {
					// 缓存已存在（其他 goroutine 已写入）
					var cached ListLatestResponse
					if err := json.Unmarshal(b, &cached); err == nil {
						return cached, nil
					}
				} else {
					// 缓存仍然未命中：查询数据库
					resp, err := doListLatestFromDB()
					if err != nil {
						return ListLatestResponse{}, err
					}
					// 写入缓存
					if b, err := json.Marshal(resp); err == nil {
						_ = f.cache.SetBytes(cacheCtx, cacheKey, b, f.cacheTTL)
					}
					return resp, nil
				}
			} else {
				// 获取锁失败：其他 goroutine 正在查询数据库
				// 短暂等待后重试（最多 5 次，每次 20 毫秒）
				for i := 0; i < 5; i++ {
					time.Sleep(20 * time.Millisecond)
					if b, err := f.cache.GetBytes(cacheCtx, cacheKey); err == nil {
						var cached ListLatestResponse
						if err := json.Unmarshal(b, &cached); err == nil {
							return cached, nil
						}
					}
				}
				// 等待超时：直接查询数据库
			}
		}
	}

	// ========== 数据库查询逻辑 ==========

	// 缓存中没有查询到结果，从数据库中查询
	resp, err := doListLatestFromDB()
	if err != nil {
		return ListLatestResponse{}, err
	}

	// 异步写入缓存（不阻塞响应）
	if cacheKey != "" {
		if b, err := json.Marshal(resp); err == nil {
			cacheCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			_ = f.cache.SetBytes(cacheCtx, cacheKey, b, f.cacheTTL)
		}
	}

	return resp, nil
}

// ============================================================================
// ============ 按点赞数查询视频 ============
// ============================================================================

// ListLikesCount 按点赞数降序查询视频（复合游标分页）
//
// 业务流程：
//   1. 从数据库查询视频（按点赞数降序，复合游标分页）
//   2. 批量查询点赞状态
//   3. 构建响应并返回
//
// 注意：
//   - 此接口不使用缓存（因为点赞数会频繁变化）
//   - 使用复合游标（点赞数 + ID）解决点赞数相同的情况
//
// 参数：
//   ctx - 上下文
//   limit - 返回的视频数量
//   cursor - 复合游标（点赞数 + ID），nil 表示第一页
//   viewerAccountID - 当前用户 ID（0 表示匿名用户）
//
// 返回：
//   ListLikesCountResponse - 响应对象
//   error - 错误信息
func (f *FeedService) ListLikesCount(ctx context.Context, limit int, cursor *LikesCountCursor, viewerAccountID uint) (ListLikesCountResponse, error) {
	// 1. 从数据库查询视频（复合游标分页）
	videos, err := f.repo.ListLikesCountWithCursor(ctx, limit, cursor)
	if err != nil {
		return ListLikesCountResponse{}, err
	}

	// 2. 判断是否还有更多数据
	hasMore := len(videos) == limit

	// 3. 批量查询点赞状态并构建 FeedVideoItem
	feedVideos, err := f.buildFeedVideos(ctx, videos, viewerAccountID)
	if err != nil {
		return ListLikesCountResponse{}, err
	}

	// 4. 构建响应对象
	resp := ListLikesCountResponse{
		VideoList: feedVideos,
		HasMore:   hasMore,
	}

	// 5. 计算下一页游标（复合游标：点赞数 + ID）
	if len(videos) > 0 {
		last := videos[len(videos)-1]
		nextLikesCountBefore := last.LikesCount
		nextIDBefore := last.ID
		resp.NextLikesCountBefore = &nextLikesCountBefore
		resp.NextIDBefore = &nextIDBefore
	}

	return resp, nil
}

// ============================================================================
// ============ 按关注列表查询视频 ============
// ============================================================================

// ListByFollowing 查询用户关注的作者的视频（带缓存和分布式锁）
//
// 业务流程：
//   1. 尝试从 Redis 缓存读取
//   2. 缓存未命中 → 加分布式锁
//   3. 查询数据库（使用子查询获取关注的作者）
//   4. 写入缓存
//   5. 批量查询点赞状态
//   6. 构建响应并返回
//
// 缓存策略：
//   - 缓存键格式：feed:listByFollowing:limit=10:accountID=123:before=0
//   - 缓存过期时间：5 秒
//   - 仅对已登录用户缓存（viewerAccountID > 0）
//
// 参数：
//   ctx - 上下文
//   limit - 返回的视频数量
//   latestBefore - 游标：上一页最后一条视频的创建时间
//   viewerAccountID - 当前用户 ID
//
// 返回：
//   ListByFollowingResponse - 响应对象
//   error - 错误信息
func (f *FeedService) ListByFollowing(ctx context.Context, limit int, latestBefore time.Time, viewerAccountID uint) (ListByFollowingResponse, error) {
	// 定义数据库查询函数（闭包）
	doListByFollowingFromDB := func() (ListByFollowingResponse, error) {
		// 1. 从数据库查询视频（使用子查询获取关注的作者）
		videos, err := f.repo.ListByFollowing(ctx, limit, viewerAccountID, latestBefore)
		if err != nil {
			return ListByFollowingResponse{}, err
		}

		// 2. 计算下一页游标
		var nextTime int64
		if len(videos) > 0 {
			nextTime = videos[len(videos)-1].CreateTime.Unix()
		} else {
			nextTime = 0
		}

		// 3. 判断是否还有更多数据
		hasMore := len(videos) == limit

		// 4. 批量查询点赞状态并构建 FeedVideoItem
		feedVideos, err := f.buildFeedVideos(ctx, videos, viewerAccountID)
		if err != nil {
			return ListByFollowingResponse{}, err
		}

		// 5. 构建响应对象
		resp := ListByFollowingResponse{
			VideoList: feedVideos,
			NextTime:  nextTime,
			HasMore:   hasMore,
		}
		return resp, nil
	}

	// ========== Redis 缓存逻辑 ==========

	// 缓存键格式：feed:listByFollowing:limit=10:accountID=123:before=0
	// 注意：仅对已登录用户缓存（viewerAccountID > 0）
	var cacheKey string
	if viewerAccountID != 0 && f.cache != nil {
		before := int64(0)
		if !latestBefore.IsZero() {
			before = latestBefore.Unix()
		}
		cacheKey = fmt.Sprintf("feed:listByFollowing:limit=%d:accountID=%d:before=%d", limit, viewerAccountID, before)

		// 设置缓存查询超时：50 毫秒
		cacheCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		// 1. 尝试从 Redis 缓存读取
		b, err := f.cache.GetBytes(cacheCtx, cacheKey)
		if err == nil {
			// 缓存命中：反序列化并返回
			var cached ListByFollowingResponse
			if err := json.Unmarshal(b, &cached); err == nil {
				return cached, nil
			}
		} else if rediscache.IsMiss(err) { // 缓存未命中
			// 分布式锁键：lock:feed:listByFollowing:limit=10:accountID=123:before=0
			lockKey := "lock:" + cacheKey

			// 2. 尝试获取分布式锁（防止缓存击穿）
			token, locked, _ := f.cache.Lock(cacheCtx, lockKey, 500*time.Millisecond)
			if locked {
				// 获取锁成功：再次检查缓存（双重检查）
				defer func() { _ = f.cache.Unlock(context.Background(), lockKey, token) }()

				if b, err := f.cache.GetBytes(cacheCtx, cacheKey); err == nil {
					// 缓存已存在（其他 goroutine 已写入）
					var cached ListByFollowingResponse
					if err := json.Unmarshal(b, &cached); err == nil {
						return cached, nil
					}
				} else {
					// 缓存仍然未命中：查询数据库
					resp, err := doListByFollowingFromDB()
					if err != nil {
						return ListByFollowingResponse{}, err
					}
					// 写入缓存
					if b, err := json.Marshal(resp); err == nil {
						_ = f.cache.SetBytes(cacheCtx, cacheKey, b, f.cacheTTL)
					}
					return resp, nil
				}
			} else {
				// 获取锁失败：其他 goroutine 正在查询数据库
				// 短暂等待后重试（最多 5 次，每次 20 毫秒）
				for i := 0; i < 5; i++ {
					time.Sleep(20 * time.Millisecond)
					if b, err := f.cache.GetBytes(cacheCtx, cacheKey); err == nil {
						var cached ListByFollowingResponse
						if err := json.Unmarshal(b, &cached); err == nil {
							return cached, nil
						}
					}
				}
				// 等待超时：直接查询数据库
			}
		}
	}

	// ========== 数据库查询逻辑 ==========

	// 缓存中没有查询到结果，从数据库中查询
	resp, err := doListByFollowingFromDB()
	if err != nil {
		return ListByFollowingResponse{}, err
	}

	// 异步写入缓存（不阻塞响应）
	if cacheKey != "" {
		if b, err := json.Marshal(resp); err == nil {
			cacheCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			defer cancel()
			_ = f.cache.SetBytes(cacheCtx, cacheKey, b, f.cacheTTL)
		}
	}

	return resp, nil
}

// ============================================================================
// ============ 按热度查询视频（Redis 热榜） ============
// ============================================================================

// ListByPopularity 按热度降序查询视频（Redis 热榜 + DB Fallback）
//
// 热榜设计说明：
//   1. 使用 Redis ZSET（有序集合）存储实时热度
//   2. 生成热榜快照（按分钟聚合，最近 60 分钟）
//   3. 使用 offset 分页（避免数据跳动）
//   4. Redis 不可用时降级到数据库查询
//
// Redis 热榜原理：
//   - 每分钟一个 ZSET：hot:video:1m:202401011500
//   - Key 格式：hot:video:1m:yyyyMMddHHmm
//   - Member：视频 ID
//   - Score：热度值
//   - 聚合快照：ZUNIONSTORE 聚合最近 60 分钟的热度
//
// 分页策略（稳定分页）：
//   - 使用 as_of（热榜快照时间）+ offset（偏移量）分页
//   - 同一个 as_of 内，offset 翻页内容不变
//   - 解决传统游标分页的问题：热度实时变化导致翻页数据跳动
//
// 业务流程：
//   1. Redis 可用：
//      a. 计算热榜快照时间（按分钟截断）
//      b. 聚合最近 60 分钟的热度数据
//      c. 使用 offset 分页获取视频 ID
//      d. 批量查询视频详细信息
//      e. 批量查询点赞状态
//      f. 构建响应并返回
//   2. Redis 不可用：
//      a. 降级到数据库查询（复合游标分页）
//      b. 使用热度 + 时间 + ID 三重游标
//
// 参数：
//   ctx - 上下文
//   limit - 返回的视频数量
//   reqAsOf - 热榜快照时间（客户端返回的，第一页传 0）
//   offset - 分页偏移量（第一页传 0）
//   viewerAccountID - 当前用户 ID
//   latestPopularity - DB Fallback 用游标：热度
//   latestBefore - DB Fallback 用游标：时间
//   latestIDBefore - DB Fallback 用游标：ID
//
// 返回：
//   ListByPopularityResponse - 响应对象
//   error - 错误信息
func (f *FeedService) ListByPopularity(ctx context.Context, limit int, reqAsOf int64, offset int, viewerAccountID uint, latestPopularity int64, latestBefore time.Time, latestIDBefore uint) (ListByPopularityResponse, error) {
	// ========== Redis 热榜查询 ==========

	if f.cache != nil {
		// 1. 计算热榜快照时间（按分钟截断）
		asOf := time.Now().UTC().Truncate(time.Minute)
		if reqAsOf > 0 {
			asOf = time.Unix(reqAsOf, 0).UTC().Truncate(time.Minute)
		}

		// 2. 聚合最近 60 分钟的热度数据
		// 聚合最近 60 个 ZSET 的键名
		const win = 60
		keys := make([]string, 0, win)
		for i := 0; i < win; i++ {
			// Key 格式：hot:video:1m:202401011500
			keys = append(keys, "hot:video:1m:"+asOf.Add(-time.Duration(i)*time.Minute).Format("200601021504"))
		}

		// 3. 生成热榜快照（ZUNIONSTORE）
		// 快照 Key 格式：hot:video:merge:1m:202401011500
		// 同一个 as_of 内，快照 Key 复用（避免重复聚合）
		dest := "hot:video:merge:1m:" + asOf.Format("200601021504")
		opCtx, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
		defer cancel()

		// 检查快照是否已存在
		exists, _ := f.cache.Exists(opCtx, dest)
		if !exists {
			// 快照不存在：聚合最近 60 分钟的热度数据（SUM 求和）
			_ = f.cache.ZUnionStore(opCtx, dest, keys, "SUM")
			// 设置快照过期时间：2 分钟（给翻页留时间）
			_ = f.cache.Expire(opCtx, dest, 2*time.Minute)
		}

		// 4. 使用 offset 分页获取视频 ID
		// ZREVRANGE：按分数降序返回指定范围的成员
		start := int64(offset)
		stop := start + int64(limit) - 1
		members, err := f.cache.ZRevRange(opCtx, dest, start, stop)

		// 处理空结果（offset 过大）
		if err == nil && len(members) == 0 {
			if offset > 0 {
				return ListByPopularityResponse{
					VideoList:  []FeedVideoItem{},
					AsOf:       asOf.Unix(),
					NextOffset: offset,
					HasMore:    false,
				}, nil
			}
		}

		// 5. 批量查询视频详细信息
		if err == nil && len(members) > 0 {
			// 解析视频 ID
			ids := make([]uint, 0, len(members))
			for _, m := range members {
				u, err := strconv.ParseUint(m, 10, 64)
				if err == nil && u > 0 {
					ids = append(ids, uint(u))
				}
			}

			// 批量查询视频
			videos, err := f.repo.GetByIDs(ctx, ids)
			if err == nil {
				// 6. 保持 Redis 返回的顺序（按热度降序）
				// 使用 map 快速查找
				byID := make(map[uint]*video.Video, len(videos))
				for _, v := range videos {
					byID[v.ID] = v
				}

				// 按 ids 的顺序构建 ordered 列表
				ordered := make([]*video.Video, 0, len(ids))
				for _, id := range ids {
					if v := byID[id]; v != nil {
						ordered = append(ordered, v)
					}
				}

				// 7. 批量查询点赞状态并构建 FeedVideoItem
				items, err := f.buildFeedVideos(ctx, ordered, viewerAccountID)
				if err != nil {
					return ListByPopularityResponse{}, err
				}

				// 8. 构建响应对象
				resp := ListByPopularityResponse{
					VideoList:  items,
					AsOf:       asOf.Unix(),
					NextOffset: offset + len(items),
					HasMore:    len(items) == limit,
				}

				// 9. 计算下一页游标（DB Fallback 用）
				if len(ordered) > 0 {
					last := ordered[len(ordered)-1]
					nextPopularity := last.Popularity
					nextBefore := last.CreateTime
					nextID := last.ID
					resp.NextLatestPopularity = &nextPopularity
					resp.NextLatestBefore = &nextBefore
					resp.NextLatestIDBefore = &nextID
				}

				return resp, nil
			}
		}
	}

	// ========== DB Fallback（Redis 不可用）==========

	// Redis 不可用时，降级到数据库查询
	videos, err := f.repo.ListByPopularity(ctx, limit, latestPopularity, latestBefore, latestIDBefore)
	if err != nil {
		return ListByPopularityResponse{}, err
	}

	// 批量查询点赞状态并构建 FeedVideoItem
	items, err := f.buildFeedVideos(ctx, videos, viewerAccountID)
	if err != nil {
		return ListByPopularityResponse{}, err
	}

	// 构建响应对象
	resp := ListByPopularityResponse{
		VideoList:  items,
		AsOf:       0,
		NextOffset: 0,
		HasMore:    len(items) == limit,
	}

	// 计算下一页游标
	if len(videos) > 0 {
		last := videos[len(videos)-1]
		nextPopularity := last.Popularity
		nextBefore := last.CreateTime
		nextID := last.ID
		resp.NextLatestPopularity = &nextPopularity
		resp.NextLatestBefore = &nextBefore
		resp.NextLatestIDBefore = &nextID
	}

	return resp, nil
}

// ============================================================================
// ============ 辅助方法：构建 FeedVideoItem ============
// ============================================================================

// buildFeedVideos 批量查询点赞状态并构建 FeedVideoItem
//
// 业务流程：
//   1. 提取所有视频 ID
//   2. 批量查询点赞状态（一次性查询，避免 N+1 问题）
//   3. 遍历视频列表，构建 FeedVideoItem
//
// N+1 问题说明：
//   - 错误做法：循环查询每个视频的点赞状态（1 次查视频 + N 次查点赞）
//   - 正确做法：批量查询所有视频的点赞状态（1 次查询搞定）
//
// 批量查询优势：
//   - 减少数据库查询次数
//   - 提升性能
//   - 降低数据库压力
//
// 参数：
//   ctx - 上下文
//   videos - 视频列表
//   viewerAccountID - 当前用户 ID（0 表示匿名用户）
//
// 返回：
//   []FeedVideoItem - FeedVideoItem 列表
//   error - 错误信息
func (f *FeedService) buildFeedVideos(ctx context.Context, videos []*video.Video, viewerAccountID uint) ([]FeedVideoItem, error) {
	// 1. 预分配内存（提升性能）
	feedVideos := make([]FeedVideoItem, 0, len(videos))

	// 2. 提取所有视频 ID
	videoIDs := make([]uint, len(videos))
	for i, v := range videos {
		videoIDs[i] = v.ID
	}

	// 3. 批量查询点赞状态（避免 N+1 问题）
	// BatchGetLiked：一次性查询多个视频的点赞状态
	likedMap, err := f.likeRepo.BatchGetLiked(ctx, videoIDs, viewerAccountID)
	if err != nil {
		return nil, err
	}

	// 4. 遍历视频列表，构建 FeedVideoItem
	for _, video := range videos {
		feedVideos = append(feedVideos, FeedVideoItem{
			ID:          video.ID,
			Author:      FeedAuthor{ID: video.AuthorID, Username: video.Username},
			Title:       video.Title,
			Description: video.Description,
			PlayURL:     video.PlayURL,
			CoverURL:    video.CoverURL,
			CreateTime:  video.CreateTime.Unix(),
			LikesCount:  video.LikesCount,
			IsLiked:     likedMap[video.ID], // 从批量查询结果中获取点赞状态
		})
	}

	return feedVideos, nil
}
