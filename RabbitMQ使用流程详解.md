# RabbitMQ 在项目中的完整使用流程

## 一、核心概念回顾

### 1.1 什么是 RabbitMQ？
RabbitMQ 是一个消息队列中间件，用于实现**异步通信**和**系统解耦**。

### 1.2 核心组件

| 组件 | 作用 | 生活中的类比 |
|------|------|------------|
| **Connection** | 底层 TCP 连接 | 手机和基站之间的连接 |
| **Channel** | 轻量级连接，用于发送/接收消息 | 通话中的语音通道 |
| **Exchange（交换机）** | 接收消息并路由到队列 | 快递分拣中心 |
| **Queue（队列）** | 存储消息，消费者从这里获取 | 快递网点 |
| **Routing Key（路由键）** | 决定消息路由到哪个队列 | 快递单上的地址 |
| **Binding（绑定）** | 将队列绑定到交换机 | 网点和分拣中心的运输线路 |
| **Producer（生产者）** | 发送消息的客户端 | 寄件人 |
| **Consumer（消费者）** | 接收并处理消息的客户端 | 收件人 |

### 1.3 三种交换机类型

| 类型 | 说明 | 应用场景 |
|------|------|---------|
| **Direct** | 精确匹配 Routing Key | 点对点通信 |
| **Fanout** | 广播，所有队列都收到 | 通知、日志 |
| **Topic** | 通配符匹配（`*` 匹配一个词，`#` 匹配多个词） | 本项目使用此类型 |

---

## 二、项目中的 RabbitMQ 架构

### 2.1 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                        Web 服务器（main.go）                  │
│                     生产者（Producer）                         │
├─────────────────────────────────────────────────────────────┤
│  HTTP 请求 → Handler → Service → 发送消息到 MQ               │
│                     ↓                                        │
│                 rabbitmq.PublishJSON()                        │
└─────────────────────────────┬───────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      RabbitMQ 服务器                          │
│                    Exchange + Queue                           │
├─────────────────────────────────────────────────────────────┤
│  Exchange("like.events") → Queue("like.events")              │
│  Exchange("comment.events") → Queue("comment.events")        │
│  Exchange("social.events") → Queue("social.events")          │
│  Exchange("video.popularity.events") → Queue(...)            │
└─────────────────────────────┬───────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      Worker 程序（worker/main.go）            │
│                      消费者（Consumer）                       │
├─────────────────────────────────────────────────────────────┤
│  Consume() → process() → 更新数据库/Redis                   │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 数据流向

```
用户点赞 → HTTP 请求 → LikeHandler → LikeService.Like()
                                              ↓
                                      LikeMQ.Like()
                                              ↓
                                      发送消息到 RabbitMQ
                                              ↓
                                      消息存储在队列中
                                              ↓
                                      LikeWorker.Consume()
                                              ↓
                                      LikeWorker.applyLike()
                                              ↓
                                      插入点赞记录、更新点赞数、更新热度
```

---

## 三、代码实现流程

### 3.1 第一步：启动 Worker（消费者）

**文件**：`backend/cmd/worker/main.go`

```go
func main() {
    // 1. 加载配置
    cfg := config.Load("configs/config.yaml")

    // 2. 连接数据库和 Redis
    sqlDB := db.NewDB(cfg.Database)
    cache := rediscache.NewFromEnv(&cfg.Redis)

    // 3. 连接 RabbitMQ
    url := "amqp://" + cfg.RabbitMQ.Username + ":" + cfg.RabbitMQ.Password + "@" + ...
    conn := amqp.Dial(url)
    ch := conn.Channel()

    // 4. 声明拓扑（Exchange + Queue + Binding）
    declareLikeTopology(ch)    // 点赞模块
    declareCommentTopology(ch) // 评论模块
    declareSocialTopology(ch)  // 关注模块
    declarePopularityTopology(ch) // 热度模块

    // 5. 设置 QoS（服务质量）
    ch.Qos(50, 0, false) // 一次性最多消费 50 条消息

    // 6. 创建 Worker 实例
    likeWorker := worker.NewLikeWorker(ch, likeRepo, videoRepo, "like.events")
    commentWorker := worker.NewCommentWorker(ch, commentRepo, videoRepo, "comment.events")
    socialWorker := worker.NewSocialWorker(ch, socialRepo, "social.events")
    popularityWorker := worker.NewPopularityWorker(ch, cache, "video.popularity.events")

    // 7. 启动 Worker（并发运行）
    go likeWorker.Run(ctx)
    go commentWorker.Run(ctx)
    go socialWorker.Run(ctx)
    go popularityWorker.Run(ctx)

    // 8. 阻塞等待退出信号
    <-errCh
}
```

**关键函数：`declareLikeTopology`**

```go
func declareLikeTopology(ch *amqp.Channel) error {
    // 1. 声明交换机
    ch.ExchangeDeclare(
        "like.events", // 交换机名称
        "topic",       // 交换机类型（Topic 模式）
        true,          // durable：持久化
        false,         // auto-delete：不自动删除
        false,         // internal：非内部交换机
        false,         // no-wait：等待服务器响应
        nil,           // args：无额外参数
    )

    // 2. 声明队列
    q := ch.QueueDeclare(
        "like.events", // 队列名称
        true,          // durable：持久化
        false,         // auto-delete：不自动删除
        false,         // exclusive：不独占
        false,         // no-wait：等待服务器响应
        nil,           // args：无额外参数
    )

    // 3. 绑定队列到交换机
    ch.QueueBind(
        q.Name,        // 队列名称
        "like.*",      // 绑定键（Routing Key 匹配规则）
        "like.events", // 交换机名称
        false,         // no-wait：等待服务器响应
        nil,           // args：无额外参数
    )
    return nil
}
```

### 3.2 第二步：Worker 消费消息

**文件**：`backend/internal/worker/likeworker.go`

```go
func (w *LikeWorker) Run(ctx context.Context) error {
    // 1. 注册消费者
    deliveries, err := w.ch.Consume(
        "like.events", // 队列名称
        "",            // 消费者标签（自动生成）
        false,         // auto-ack：手动确认
        false,         // exclusive：不独占
        false,         // no-local：不限制本地消息
        false,         // no-wait：等待服务器响应
        nil,           // args：无额外参数
    )

    // 2. 循环处理消息
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case d := <-deliveries:
            w.handleDelivery(ctx, d)
        }
    }
}

func (w *LikeWorker) handleDelivery(ctx context.Context, d amqp.Delivery) {
    // 处理消息
    if err := w.process(ctx, d.Body); err != nil {
        // 处理失败，NACK（重新入队）
        d.Nack(false, true)
        return
    }
    // 处理成功，ACK（确认并删除消息）
    d.Ack(false)
}

func (w *LikeWorker) process(ctx context.Context, body []byte) error {
    // 1. 反序列化 JSON
    var evt rabbitmq.LikeEvent
    json.Unmarshal(body, &evt)

    // 2. 根据 Action 分发处理
    switch evt.Action {
    case "like":
        w.applyLike(ctx, evt.UserID, evt.VideoID)
    case "unlike":
        w.applyUnlike(ctx, evt.UserID, evt.VideoID)
    }
}

func (w *LikeWorker) applyLike(ctx context.Context, userID, videoID uint) error {
    // 1. 检查视频是否存在
    w.videos.IsExist(ctx, videoID)

    // 2. 插入点赞记录（忽略重复）
    w.likes.LikeIgnoreDuplicate(ctx, &video.Like{...})

    // 3. 更新点赞数（+1）
    w.videos.ChangeLikesCount(ctx, videoID, 1)

    // 4. 更新热度（+1）
    w.videos.ChangePopularity(ctx, videoID, 1)
}
```

### 3.3 第三步：启动 Web 服务器（生产者）

**文件**：`backend/cmd/main.go`

```go
func main() {
    // 1. 加载配置
    cfg := config.Load("configs/config.yaml")

    // 2. 连接数据库和 Redis
    sqlDB := db.NewDB(cfg.Database)
    cache := rediscache.NewFromEnv(&cfg.Redis)

    // 3. 连接 RabbitMQ（作为生产者）
    rmq := rabbitmq.NewRabbitMQ(&cfg.RabbitMQ)
    defer rmq.Close()

    // 4. 设置路由（初始化 Service，注入 RMQ）
    r := http.SetRouter(sqlDB, cache, rmq)

    // 5. 启动 HTTP 服务器
    r.Run(":8080")
}
```

### 3.4 第四步：初始化 MQ 拓扑（生产者端）

**文件**：`backend/internal/http/router.go`

```go
func SetRouter(db, cache, rmq) *gin.Engine {
    // ...

    // 初始化点赞 MQ
    likeMQ, err := rabbitmq.NewLikeMQ(rmq)
    // 内部会声明 Exchange("like.events")、Queue("like.events")、Binding("like.*")

    // 初始化评论 MQ
    commentMQ, err := rabbitmq.NewCommentMQ(rmq)
    // 内部会声明 Exchange("comment.events")、Queue("comment.events")、Binding("comment.*")

    // 初始化关注 MQ
    socialMQ, err := rabbitmq.NewSocialMQ(rmq)
    // 内部会声明 Exchange("social.events")、Queue("social.events")、Binding("social.*")

    // 初始化热度 MQ
    popularityMQ, err := rabbitmq.NewPopularityMQ(rmq)
    // 内部会声明 Exchange("video.popularity.events")、Queue(...)、Binding("video.popularity.*")

    // 创建 Service（注入 MQ）
    likeService := video.NewLikeService(likeRepo, videoRepo, cache, likeMQ, popularityMQ)
    commentService := video.NewCommentService(commentRepo, videoRepo, cache, commentMQ, popularityMQ)
    socialService := social.NewSocialService(socialRepo, accountRepo, socialMQ)

    // ...
}
```

### 3.5 第五步：Service 发送消息到 MQ

**文件**：`backend/internal/video/like_service.go`

```go
func (s *LikeService) Like(ctx context.Context, like *Like) error {
    // 1. 校验参数
    if like.VideoID == 0 || like.AccountID == 0 {
        return errors.New("invalid params")
    }

    // 2. 校验视频是否存在
    s.VideoRepo.IsExist(ctx, like.VideoID)

    // 3. 校验是否已点赞
    isLiked := s.repo.IsLiked(ctx, like.VideoID, like.AccountID)
    if isLiked {
        return errors.New("already liked")
    }

    // 4. 尝试发送 MQ 消息
    mysqlEnqueued := false   // 点赞 MQ 是否成功
    redisEnqueued := false   // 热度 MQ 是否成功

    // 发送点赞消息到 MQ
    if s.likeMQ != nil {
        if err := s.likeMQ.Like(ctx, like.AccountID, like.VideoID); err == nil {
            mysqlEnqueued = true
        }
    }

    // 发送热度更新消息到 MQ
    if s.popularityMQ != nil {
        if err := s.popularityMQ.Update(ctx, like.VideoID, 1); err == nil {
            redisEnqueued = true
        }
    }

    // 5. 两个 MQ 都成功，直接返回（Worker 会异步处理）
    if mysqlEnqueued && redisEnqueued {
        return nil
    }

    // 6. Fallback：MQ 失败，降级到直接写数据库
    if !mysqlEnqueued {
        s.repo.db.Transaction(func(tx) {
            tx.Create(like)
            tx.Model(&Video{}).UpdateColumn("likes_count", gorm.Expr("likes_count + 1"))
            tx.Model(&Video{}).UpdateColumn("popularity", gorm.Expr("popularity + 1"))
        })
    }

    // 7. Fallback：热度 MQ 失败，直接更新 Redis
    if !redisEnqueued {
        UpdatePopularityCache(ctx, s.cache, like.VideoID, 1)
    }

    return nil
}
```

### 3.6 第六步：MQ 封装层发送消息

**文件**：`backend/internal/middleware/rabbitmq/likeMQ.go`

```go
func (l *LikeMQ) Like(ctx context.Context, userID, videoID uint) error {
    return l.publish(ctx, "like", "like.like", userID, videoID)
}

func (l *LikeMQ) publish(ctx context.Context, action, routingKey string, userID, videoID uint) error {
    // 1. 生成事件 ID
    eventID, _ := newEventID(16)

    // 2. 构造事件对象
    event := LikeEvent{
        EventID:    eventID,
        Action:     action,           // "like" 或 "unlike"
        UserID:     userID,
        VideoID:    videoID,
        OccurredAt: time.Now(),
    }

    // 3. 发布到 MQ
    return l.PublishJSON(ctx, "like.events", routingKey, event)
}
```

**文件**：`backend/internal/middleware/rabbitmq/rabbitMQ.go`

```go
func (r *RabbitMQ) PublishJSON(ctx context.Context, exchange string, routingKey string, payload any) error {
    // 1. 序列化为 JSON
    b, _ := json.Marshal(payload)

    // 2. 发布消息
    return r.ch.PublishWithContext(ctx,
        exchange,    // 交换机名称
        routingKey,  // 路由键
        false,       // mandatory：消息无法路由时是否返回
        false,       // immediate：消息无法立即投递时是否返回
        amqp.Publishing{
            ContentType:  "application/json", // 内容类型
            DeliveryMode: amqp.Persistent,    // 持久化模式
            Timestamp:    time.Now(),         // 时间戳
            Body:         b,                  // 消息体
        },
    )
}
```

---

## 四、完整流程图

### 4.1 点赞流程

```
用户点击"点赞"
    ↓
POST /like/like
    ↓
LikeHandler.Like()
    ↓
LikeService.Like()
    ↓
校验视频是否存在、是否已点赞
    ↓
┌─────────────────────────────┐
│  优先尝试：发送 MQ 消息     │
│  (异步处理，响应快)          │
└─────────────────────────────┘
    ↓
LikeMQ.Like() → 发送到 Exchange("like.events")
PopularityMQ.Update() → 发送到 Exchange("video.popularity.events")
    ↓
消息存储在队列中
    ↓
Worker.Consume() → 获取消息
    ↓
LikeWorker.process() → 反序列化 JSON
    ↓
LikeWorker.applyLike() → 处理业务逻辑
    ↓
插入点赞记录 → 更新点赞数 → 更新热度
    ↓
Worker.Ack() → 确认消息处理完成
```

### 4.2 MQ 失败时的 Fallback 流程

```
LikeService.Like()
    ↓
尝试发送 MQ 消息
    ↓
发送失败（MQ 不可用）
    ↓
┌─────────────────────────────┐
│  Fallback 降级机制           │
│  (直接写数据库，保证可用性)  │
└─────────────────────────────┘
    ↓
数据库事务：
  - 插入点赞记录
  - 更新点赞数（likes_count + 1）
  - 更新热度（popularity + 1）
    ↓
更新 Redis 缓存（热度）
    ↓
返回成功
```

---

## 五、核心参数说明

### 5.1 ExchangeDeclare 参数

```go
ch.ExchangeDeclare(
    "like.events", // name: 交换机名称
    "topic",       // kind: 交换机类型
    true,          // durable: 持久化（RabbitMQ 重启后交换机仍存在）
    false,         // autoDelete: 自动删除（没有队列绑定时删除）
    false,         // internal: 内部交换机（不允许客户端直接发送消息）
    false,         // noWait: 异步声明（不等待服务器响应）
    nil,           // args: 额外参数（如 TTL、死信队列等）
)
```

### 5.2 QueueDeclare 参数

```go
ch.QueueDeclare(
    "like.events", // name: 队列名称
    true,          // durable: 持久化（RabbitMQ 重启后队列仍存在）
    false,         // autoDelete: 自动删除（没有消费者时删除）
    false,         // exclusive: 独占队列（仅限当前连接使用）
    false,         // noWait: 异步声明（不等待服务器响应）
    nil,           // args: 额外参数
)
```

### 5.3 QueueBind 参数

```go
ch.QueueBind(
    "like.events", // queue: 队列名称
    "like.*",      // key: 绑定键（Routing Key）
    "like.events", // exchange: 交换机名称
    false,         // noWait: 异步绑定（不等待服务器响应）
    nil,           // args: 额外参数
)
```

### 5.4 Consume 参数

```go
ch.Consume(
    "like.events", // queue: 队列名称
    "",            // consumer: 消费者标签（空字符串表示自动生成）
    false,         // autoAck: 自动确认（false 表示手动确认）
    false,         // exclusive: 独占模式（仅此消费者可以消费该队列）
    false,         // noLocal: 不允许接收本连接发布的消息
    false,         // noWait: 异步消费（不等待服务器响应）
    nil,           // args: 额外参数
)
```

### 5.5 Publish 参数

```go
ch.PublishWithContext(
    ctx,           // context: 上下文（用于超时控制）
    "like.events", // exchange: 交换机名称
    "like.like",   // key: 路由键
    false,         // mandatory: 消息无法路由时是否返回
    false,         // immediate: 消息无法立即投递时是否返回
    amqp.Publishing{
        ContentType:  "application/json",  // 内容类型
        DeliveryMode: amqp.Persistent,     // 持久化模式（消息不丢失）
        Timestamp:    time.Now(),           // 时间戳
        Body:         []byte(`{...}`),     // 消息体
    },
)
```

---

## 六、Topic 模式的路由规则

### 6.1 通配符说明

| 符号 | 含义 | 示例 |
|------|------|------|
| `*` | 匹配一个单词 | `like.*` 可以匹配 `like.like`、`like.unlike` |
| `#` | 匹配零个或多个单词 | `video.#` 可以匹配 `video`、`video.popularity`、`video.popularity.update` |

### 6.2 项目中的绑定规则

| Exchange | Binding Key | Queue | 说明 |
|----------|-------------|-------|------|
| `like.events` | `like.*` | `like.events` | 所有以 `like.` 开头的消息 |
| `comment.events` | `comment.*` | `comment.events` | 所有以 `comment.` 开头的消息 |
| `social.events` | `social.*` | `social.events` | 所有以 `social.` 开头的消息 |
| `video.popularity.events` | `video.popularity.*` | `video.popularity.events` | 所有以 `video.popularity.` 开头的消息 |

### 6.3 路由示例

| Routing Key | 匹配的队列 |
|-------------|-----------|
| `like.like` | `like.events` |
| `like.unlike` | `like.events` |
| `comment.publish` | `comment.events` |
| `comment.delete` | `comment.events` |
| `social.follow` | `social.events` |
| `social.unfollow` | `social.events` |
| `video.popularity.update` | `video.popularity.events` |

---

## 七、消息可靠性保证

### 7.1 消息持久化

```go
// 1. 交换机持久化
ch.ExchangeDeclare(exchange, "topic", true, ...) // 第三个参数 durable=true

// 2. 队列持久化
ch.QueueDeclare(queue, true, ...) // 第二个参数 durable=true

// 3. 消息持久化
ch.Publish(..., amqp.Publishing{
    DeliveryMode: amqp.Persistent, // 持久化消息
})
```

### 7.2 消息确认机制

```go
// 生产者：设置 mandatory
// 如果消息无法路由，返回给生产者
ch.Publish(exchange, routingKey, true, ...)

// 消费者：手动 ACK
ch.Consume(queue, "", false, ...) // autoAck=false

// 处理成功：ACK
d.Ack(false) // 确认并删除消息

// 处理失败：NACK
d.Nack(false, true) // 拒绝并重新入队
```

### 7.3 Fallback 降级机制

```go
// 优先使用 MQ
if mq != nil {
    if err := mq.Publish(...); err == nil {
        return nil // MQ 成功，直接返回
    }
}

// MQ 失败，降级到直接写数据库
db.Transaction(func(tx) {
    tx.Create(record)
    tx.Model(...).Update(...)
})
```

---

## 八、常见问题

### Q1: 为什么需要两个程序（main.go 和 worker/main.go）？

**答**：这是典型的**生产者-消费者**架构：
- **main.go**：Web 服务器，处理用户请求，发送消息到 MQ（生产者）
- **worker/main.go**：后台服务，消费 MQ 消息，更新数据库（消费者）

这样设计的优点：
1. **提升响应速度**：用户请求快速返回，数据库操作异步完成
2. **降低耦合**：点赞模块不需要知道热度模块的实现
3. **提高可靠性**：MQ 消息持久化，即使 Worker 挂了，消息也不会丢失

### Q2: 为什么 Worker 要单独运行，不放在 main.go 里？

**答**：分离 Worker 的好处：
1. **独立扩展**：Worker 和 Web 服务器可以独立扩容
2. **资源隔离**：Worker 消耗大量 CPU/内存，不会影响 Web 服务器
3. **故障隔离**：Worker 挂了不会影响用户请求

### Q3: 什么是 Fallback 降级机制？

**答**：当 MQ 不可用时，降级到直接写数据库，保证功能可用：

```
正常情况：Service → MQ → Worker → Database
降级情况：Service → Database（直接写）
```

### Q4: ACK 和 NACK 有什么区别？

**答**：
- **ACK**：确认消息处理成功，RabbitMQ 从队列删除消息
- **NACK**：确认消息处理失败，RabbitMQ 将消息重新入队（下次再消费）

### Q5: QoS(50, 0, false) 是什么意思？

**答**：设置消费者的服务质量
- `50`：一次性最多预取 50 条消息（防止消息堆积在内存）
- `0`：不限制消息字节数
- `false`：不应用到全局连接（只应用到当前通道）

---

## 九、总结

### 9.1 RabbitMQ 的使用步骤

1. **连接 RabbitMQ**：`amqp.Dial(url)`
2. **创建通道**：`conn.Channel()`
3. **声明交换机**：`ch.ExchangeDeclare(...)`
4. **声明队列**：`ch.QueueDeclare(...)`
5. **绑定队列**：`ch.QueueBind(...)`
6. **发送消息**（生产者）：`ch.Publish(...)`
7. **消费消息**（消费者）：`ch.Consume(...)`
8. **确认消息**（消费者）：`d.Ack()` 或 `d.Nack()`

### 9.2 项目中的 MQ 使用模式

```
┌──────────────┐        ┌──────────────┐        ┌──────────────┐
│   Producer   │  发送   │   Exchange   │  路由到 │    Queue     │
│  (main.go)   │ ────→  │ (like.events)│ ────→  │(like.events) │
└──────────────┘        └──────────────┘        └──────┬───────┘
                                                          │
                                                          │ 消费
                                                          ▼
┌──────────────┐        ┌──────────────┐        ┌──────────────┐
│  Consumer    │  获取   │    Worker    │  处理   │  Database    │
│(worker/main) │ ←────  │ (likeworker) │ ────→  │  (MySQL)     │
└──────────────┘        └──────────────┘        └──────────────┘
```

### 9.3 关键代码文件清单

| 文件 | 作用 | 角色 |
|------|------|------|
| `backend/cmd/main.go` | 启动 Web 服务器，初始化 MQ | Producer |
| `backend/internal/http/router.go` | 初始化 MQ 拓扑，注入到 Service | Producer |
| `backend/internal/middleware/rabbitmq/*.go` | MQ 封装层，提供发送消息接口 | Producer |
| `backend/internal/*/service.go` | 业务逻辑层，调用 MQ 发送消息 | Producer |
| `backend/cmd/worker/main.go` | 启动 Worker，消费 MQ 消息 | Consumer |
| `backend/internal/worker/*.go` | Worker 实现类，处理消息 | Consumer |
