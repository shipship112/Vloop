# Feed 流模块 - 完整详解

## 一、模块概览

### 1.1 什么是 Feed 流？

**Feed 流** = **个性化推荐列表**

生活中的例子：
- 抖音首页的推荐视频
- 微信朋友圈的时间线
- 小红书发现页的推荐内容

### 1.2 Feed 流的核心价值

| 价值 | 说明 |
|------|------|
| **用户体验** | 用户打开 APP 就能看到喜欢的内容 |
| **停留时间** | 好的 Feed 流让用户停留更久 |
| **商业价值** | 更多曝光 = 更多变现机会 |

### 1.3 项目的 Feed 流接口

| 接口 | 功能 | 是否需要登录 | 缓存策略 |
|------|------|------------|---------|
| `ListLatest` | 最新视频 | 否 | Redis 缓存（匿名用户） |
| `ListByFollowing` | 关注列表 | 是 | Redis 缓存（已登录用户） |
| `ListByPopularity` | 热门视频 | 否 | Redis 热榜（优先）+ DB Fallback |
| `ListLikesCount` | 点赞排行 | 否 | 不缓存（实时数据） |

---

## 二、核心概念 - 游标分页

### 2.1 什么是游标分页？

**传统分页（OFFSET + LIMIT）**：

```
第 1 页：SELECT * FROM videos ORDER BY create_time DESC LIMIT 10 OFFSET 0
         结果：[视频1, 视频2, ..., 视频10]

第 2 页：SELECT * FROM videos ORDER BY create_time DESC LIMIT 10 OFFSET 10
         结果：[视频11, 视频12, ..., 视频20]

❌ 问题：在第 1 页和第 2 页之间，有人发布了新视频
        第 2 页会漏掉一些视频，或者看到重复的视频
```

**游标分页（Cursor-based Pagination）**：

```
第 1 页：SELECT * FROM videos
         WHERE create_time < NOW()
         ORDER BY create_time DESC
         LIMIT 10
         结果：[视频1, 视频2, ..., 视频10]
         返回游标：next_time = 视频10.create_time

第 2 页：SELECT * FROM videos
         WHERE create_time < 视频10.create_time  ← 使用游标
         ORDER BY create_time DESC
         LIMIT 10
         结果：[视频11, 视频12, ..., 视频20]

✅ 优势：即使中间有新视频，也不会重复或遗漏
```

### 2.2 为什么需要复合游标？

当多个数据的排序字段相同时，需要使用**复合游标**：

```
场景：按点赞数排序，有多个视频点赞数都是 1000

单字段游标（❌ 不行）：
  游标：likes_count = 1000
  问题：无法区分点赞数都是 1000 的视频

复合游标（✅ 可行）：
  游标：likes_count = 1000, id = 123
  逻辑：
    - likes_count < 1000  → 查询
    - likes_count = 1000 AND id < 123  → 查询
  优势：通过 ID 区分相同点赞数的视频
```

### 2.3 项目中的游标设计

| 接口 | 游标字段 | 游标类型 | 说明 |
|------|---------|---------|------|
| `ListLatest` | `next_time` | 单字段 | 按时间分页 |
| `ListByFollowing` | `next_time` | 单字段 | 按时间分页 |
| `ListByPopularity` | `as_of` + `offset` | 快照 + 偏移 | 热榜稳定分页 |
| `ListLikesCount` | `next_likes_count_before` + `next_id_before` | 复合字段 | 点赞数 + ID |

---

## 三、代码结构解析

### 3.1 分层架构

```
Handler 层（HTTP 接口）
    ↓
Service 层（业务逻辑）
    ↓
Repository 层（数据库查询）
```

### 3.2 各层职责

| 层 | 职责 | 文件 |
|----|------|------|
| **Handler** | 接收 HTTP 请求、解析参数、调用 Service、返回响应 | `handler.go` |
| **Service** | 业务逻辑、缓存处理、批量查询、构建响应 | `service.go` |
| **Repository** | 执行 SQL 查询、返回数据 | `repo.go` |
| **Entity** | 数据模型、请求/响应结构 | `entity.go` |

---

## 四、Handler 层详解

### 4.1 ListLatest（最新视频）

```go
func (f *FeedHandler) ListLatest(c *gin.Context) {
    // 1. 解析请求参数
    var req ListLatestRequest
    c.ShouldBindJSON(&req)

    // 2. 校验 limit（防止一次查询过多数据）
    if req.Limit <= 0 || req.Limit > 50 {
        req.Limit = 10
    }

    // 3. 转换游标时间戳
    var latestTime time.Time
    if req.LatestTime > 0 {
        latestTime = time.Unix(req.LatestTime, 0)
    }

    // 4. 获取当前用户 ID（可选，用于查询点赞状态）
    viewerAccountID, _ := jwt.GetAccountID(c)
    if err != nil {
        viewerAccountID = 0
    }

    // 5. 调用 Service 层查询视频
    feedItems, err := f.service.ListLatest(...)

    // 6. 返回响应
    c.JSON(200, feedItems)
}
```

### 4.2 请求/响应示例

```json
// 请求（第一页）
{
    "limit": 10,
    "latest_time": 0
}

// 响应
{
    "video_list": [
        {
            "id": 123,
            "author": {"id": 456, "username": "张三"},
            "title": "猫咪跳舞",
            "description": "超可爱的猫咪",
            "play_url": "http://.../video.mp4",
            "cover_url": "http://.../cover.jpg",
            "create_time": 1640000000,
            "likes_count": 12000,
            "is_liked": false
        }
    ],
    "next_time": 1639999500,
    "has_more": true
}

// 请求（第二页）
{
    "limit": 10,
    "latest_time": 1639999500  // 使用上一页返回的 next_time
}
```

---

## 五、Service 层详解

### 5.1 Redis 缓存策略

#### 缓存键设计

```go
// 最新视频（匿名用户）
cacheKey := "feed:listLatest:limit=10:before=0"

// 关注列表（已登录用户）
cacheKey := "feed:listByFollowing:limit=10:accountID=123:before=0"
```

#### 缓存过期时间

```go
cacheTTL := 5 * time.Second  // 5 秒
```

为什么是 5 秒？
- 平衡新鲜度和性能
- 热门数据不会变化太快
- 减少数据库压力

#### 缓存策略对比

| 策略 | 说明 | 优势 | 劣势 |
|------|------|------|------|
| **只缓存匿名用户** | `ListLatest` 和 `ListByPopularity` | 减少缓存键数量 | 已登录用户无法享受缓存 |
| **缓存已登录用户** | `ListByFollowing` | 个性化体验 | 缓存键数量多 |

### 5.2 分布式锁防止缓存击穿

#### 什么是缓存击穿？

```
场景：缓存过期，大量并发同时请求

请求1 → 缓存过期 → 查询数据库 → 写入缓存
请求2 → 缓存过期 → 查询数据库 → 写入缓存
请求3 → 缓存过期 → 查询数据库 → 写入缓存
...
请求1000 → 缓存过期 → 查询数据库 → 写入缓存

❌ 问题：1000 个请求同时查询数据库，数据库压力暴增
```

#### 分布式锁解决方案

```
请求1 → 获取锁成功 → 查询数据库 → 写入缓存 → 释放锁
请求2 → 获取锁失败 → 等待 → 读取缓存 → 返回
请求3 → 获取锁失败 → 等待 → 读取缓存 → 返回
...
请求1000 → 获取锁失败 → 等待 → 读取缓存 → 返回

✅ 结果：只有 1 个请求查询数据库，其他请求等待并读取缓存
```

#### 代码实现

```go
// 1. 尝试获取分布式锁
lockKey := "lock:" + cacheKey
token, locked, _ := f.cache.Lock(ctx, lockKey, 500*time.Millisecond)

if locked {
    // 2. 获取锁成功：再次检查缓存（双重检查）
    if b, err := f.cache.GetBytes(ctx, cacheKey); err == nil {
        return cached, nil
    }

    // 3. 查询数据库
    resp, err := doListLatestFromDB()

    // 4. 写入缓存
    f.cache.SetBytes(ctx, cacheKey, b, f.cacheTTL)

    // 5. 释放锁
    defer f.cache.Unlock(context.Background(), lockKey, token)
} else {
    // 6. 获取锁失败：等待并重试
    for i := 0; i < 5; i++ {
        time.Sleep(20 * time.Millisecond)
        if b, err := f.cache.GetBytes(ctx, cacheKey); err == nil {
            return cached, nil
        }
    }
}
```

### 5.3 批量查询避免 N+1 问题

#### 什么是 N+1 问题？

```
❌ 错误做法：循环查询
for _, video := range videos {
    isLiked := likeRepo.IsLiked(video.ID, userID)  // N 次查询
}
结果：1 次查视频 + N 次查点赞 = N+1 次查询

✅ 正确做法：批量查询
videoIDs := []uint{1, 2, 3, ..., N}
likedMap := likeRepo.BatchGetLiked(videoIDs, userID)  // 1 次查询
结果：1 次查视频 + 1 次查点赞 = 2 次查询
```

#### 代码实现

```go
func (f *FeedService) buildFeedVideos(ctx context.Context, videos []*video.Video, viewerAccountID uint) ([]FeedVideoItem, error) {
    // 1. 提取所有视频 ID
    videoIDs := make([]uint, len(videos))
    for i, v := range videos {
        videoIDs[i] = v.ID
    }

    // 2. 批量查询点赞状态
    likedMap, err := f.likeRepo.BatchGetLiked(ctx, videoIDs, viewerAccountID)

    // 3. 遍历视频列表，构建 FeedVideoItem
    for _, video := range videos {
        feedVideos = append(feedVideos, FeedVideoItem{
            IsLiked: likedMap[video.ID],  // 从批量查询结果中获取
        })
    }

    return feedVideos, nil
}
```

---

## 六、Redis 热榜设计

### 6.1 为什么使用快照 + Offset？

#### 传统游标分页的问题

```
问题：热度实时变化，翻页时数据会乱

第 1 页（10:00:00）：
  热度：[100, 90, 80, 70, 60]

第 2 页（10:00:05）：
  热度：[50, 65, 40, 30, 20]  ← 65 的热度从 60 涨上来了

❌ 问题：翻页时数据会变化，体验不好
```

#### 快照 + Offset 的优势

```
快照时间：10:00:00（按分钟截断）

第 1 页（10:00:00）：
  快照：[100, 90, 80, 70, 60]
  offset: 0

第 2 页（10:00:01）：
  快照：[100, 90, 80, 70, 60]  ← 使用同一个快照
  offset: 10

✅ 优势：热榜内容稳定，翻页时不会跳动
```

### 6.2 Redis 数据结构

#### ZSET（有序集合）

```
Key: hot:video:1m:202401011500
Member: 视频 ID
Score: 热度值

示例：
  hot:video:1m:202401011500 {
    "123": 1000,
    "456": 800,
    "789": 600
  }
```

#### 聚合最近 60 分钟的数据

```
计算 10:00:00 的热榜快照：

聚合以下 60 个 ZSET：
  hot:video:1m:202401011500  ← 当前分钟
  hot:video:1m:202401011499
  hot:video:1m:202401011498
  ...
  hot:video:1m:202401011441  ← 60 分钟前

聚合结果：
  hot:video:merge:1m:202401011500 {
    "123": 1000 + 900 + ...  ← 求和（SUM）
    "456": 800 + 700 + ...
  }
```

### 6.3 代码实现

```go
// 1. 计算热榜快照时间（按分钟截断）
asOf := time.Now().UTC().Truncate(time.Minute)

// 2. 聚合最近 60 分钟的热度数据
keys := make([]string, 0, 60)
for i := 0; i < 60; i++ {
    keys = append(keys, "hot:video:1m:"+asOf.Add(-time.Duration(i)*time.Minute).Format("200601021504"))
}

// 3. 生成快照（ZUNIONSTORE）
dest := "hot:video:merge:1m:" + asOf.Format("200601021504")
f.cache.ZUnionStore(ctx, dest, keys, "SUM")  // 求和聚合

// 4. 设置快照过期时间：2 分钟（给翻页留时间）
f.cache.Expire(ctx, dest, 2*time.Minute)

// 5. 使用 offset 分页获取视频 ID
start := int64(offset)
stop := start + int64(limit) - 1
members, _ := f.cache.ZRevRange(ctx, dest, start, stop)
```

---

## 七、Repository 层详解

### 7.1 复合游标 SQL 查询

#### 点赞排行（复合游标）

```sql
SELECT * FROM videos
WHERE
  (likes_count < ?) OR
  (likes_count = ? AND id < ?)
ORDER BY likes_count DESC, id DESC
LIMIT 10;
```

#### 热度排行（三重游标）

```sql
SELECT * FROM videos
WHERE
  (popularity < ?) OR
  (popularity = ? AND create_time < ?) OR
  (popularity = ? AND create_time = ? AND id < ?)
ORDER BY popularity DESC, create_time DESC, id DESC
LIMIT 10;
```

### 7.2 关注列表（子查询）

```sql
SELECT * FROM videos
WHERE author_id IN (
  SELECT vlogger_id FROM socials
  WHERE follower_id = ?
)
ORDER BY create_time DESC
LIMIT 10;
```

---

## 八、完整业务流程

### 8.1 最新视频流程

```
用户请求 → Handler → Service → Redis 缓存
                              ↓ 缓存未命中
                        分布式锁
                              ↓ 获取锁成功
                        查询数据库
                              ↓
                        批量查询点赞状态
                              ↓
                        构建响应
                              ↓
                        写入缓存
                              ↓
                        返回响应
```

### 8.2 热门视频流程

```
用户请求 → Handler → Service → Redis 热榜
                              ↓
                        计算快照时间
                              ↓
                        聚合 60 分钟数据
                              ↓
                        使用 offset 分页
                              ↓
                        批量查询视频详情
                              ↓
                        批量查询点赞状态
                              ↓
                        构建响应
                              ↓
                        返回响应
```

---

## 九、面试重点

### 9.1 游标分页 vs 传统分页

| 对比点 | 游标分页 | 传统分页 |
|-------|---------|---------|
| **性能** | OFFSET 越大越慢 | 性能稳定 |
| **数据一致性** | 无重复/遗漏 | 可能有重复/遗漏 |
| **适用场景** | 大数据量、实时更新 | 小数据量、静态数据 |

### 9.2 缓存击穿及解决方案

**问题**：缓存过期时，大量并发同时查询数据库

**解决方案**：
1. 分布式锁（本项目使用）
2. 缓存预热
3. 互斥单线程

### 9.3 N+1 问题及解决方案

**问题**：循环查询导致性能低下

**解决方案**：
1. 批量查询（本项目使用）
2. 使用 JOIN
3. 使用 IN 子查询

### 9.4 热榜设计思路

**问题**：热度实时变化，翻页数据跳动

**解决方案**：
1. 生成热榜快照（按分钟截断）
2. 使用 offset 分页
3. 快照过期时间：2 分钟（给翻页留时间）

---

## 十、总结

### 10.1 Feed 流模块的核心技术点

1. **游标分页**：避免数据重复/遗漏
2. **复合游标**：处理排序字段相同的情况
3. **Redis 缓存**：提升性能，减少数据库压力
4. **分布式锁**：防止缓存击穿
5. **批量查询**：避免 N+1 问题
6. **热榜快照**：稳定分页，避免数据跳动

### 10.2 代码文件清单

| 文件 | 行数 | 职责 |
|------|------|------|
| `entity.go` | 83 | 数据模型、请求/响应结构 |
| `handler.go` | 165 | HTTP 接口处理器 |
| `service.go` | 366 | 业务逻辑层 |
| `repo.go` | 104 | 数据访问层 |

### 10.3 学习建议

1. **先理解业务**：Feed 流是什么，解决了什么问题
2. **再理解设计**：游标分页、缓存策略、热榜设计
3. **最后看代码**：Handler → Service → Repository → Entity
4. **画流程图**：把业务流程画出来，加深理解
5. **动手实践**：自己写一个简单的 Feed 流接口

---

**恭喜！你已经完全理解了 Feed 流模块！** 🎉

这个模块整合了前面所有模块的知识点：
- Account（用户信息）
- Video（视频数据）
- Like（点赞状态）
- Social（关注关系）
- Redis（缓存）
- SQL（数据库查询）

**现在你可以自信地在面试中讲解这个模块了！** 💪
