// Package main 是 Worker 程序的入口
// Worker 的作用：作为消费者，监听 RabbitMQ 队列中的消息并异步处理业务逻辑
// 比如点赞消息、评论消息、关注消息等
package main

import (
	"context"
	"feedsystem_video_go/internal/config"
	"feedsystem_video_go/internal/db"
	rediscache "feedsystem_video_go/internal/middleware/redis"
	"feedsystem_video_go/internal/social"
	"feedsystem_video_go/internal/video"
	"feedsystem_video_go/internal/worker"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ 拓扑结构常量定义
// 拓扑 = Exchange（交换机） + Queue（队列） + Binding（绑定关系）

// ============ Social 关注模块 ============
const (
	// 交换机名称：所有关注相关事件都发到这里
	socialExchange = "social.events"
	// 队列名称：存储关注事件的队列
	socialQueue = "social.events"
	// 绑定键：通配符匹配 "social.*"（所有以 social. 开头的路由键）
	socialBindingKey = "social.*"
)

// ============ Like 点赞模块 ============
const (
	likeExchange   = "like.events"
	likeQueue      = "like.events"
	likeBindingKey = "like.*"
)

// ============ Comment 评论模块 ============
const (
	commentExchange   = "comment.events"
	commentQueue      = "comment.events"
	commentBindingKey = "comment.*"
)

// ============ Popularity 热度模块 ============
const (
	popularityExchange   = "video.popularity.events"
	popularityQueue      = "video.popularity.events"
	popularityBindingKey = "video.popularity.*"
)

func main() {
	// ========== 1. 初始化配置和基础连接 ==========

	// 加载配置文件
	log.Printf("Loading config from configs/config.yaml")
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 连接 MySQL 数据库
	sqlDB, err := db.NewDB(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect database: %v", err)
	}
	defer db.CloseDB(sqlDB)

	// 连接 Redis（用于热度计算和缓存）
	// 如果 Redis 不可用，热度 Worker 会被禁用，但其他 Worker 仍可运行
	cache, err := rediscache.NewFromEnv(&cfg.Redis)
	if err != nil {
		log.Printf("Redis config error (popularity worker disabled): %v", err)
		cache = nil
	} else {
		pingCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if err := cache.Ping(pingCtx); err != nil {
			log.Printf("Redis not available (popularity worker disabled): %v", err)
			_ = cache.Close()
			cache = nil
		} else {
			defer cache.Close()
			log.Printf("Redis connected (popularity worker enabled)")
		}
	}

	// ========== 2. 连接 RabbitMQ ==========

	// 构建 RabbitMQ 连接字符串
	// 格式：amqp://用户名:密码@主机:端口/
	url := "amqp://" + cfg.RabbitMQ.Username + ":" + cfg.RabbitMQ.Password + "@" + cfg.RabbitMQ.Host + ":" + strconv.Itoa(cfg.RabbitMQ.Port) + "/"

	// 建立连接（底层 TCP 连接）
	// 注意：conn 是长期连接，整个程序运行期间保持打开
	conn, err := amqp.Dial(url)
	if err != nil {
		log.Fatalf("Failed to connect rabbitmq: %v", err)
	}
	defer conn.Close()

	// 创建通道（Channel）
	// 通道是轻量级的连接，可以创建多个，推荐每个 goroutine 使用一个通道
	// 注意：通道不是线程安全的，不要在多个 goroutine 中共享
	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open rabbitmq channel: %v", err)
	}
	defer ch.Close()

	// ========== 3. 声明 RabbitMQ 拓扑结构 ==========

	// 什么是拓扑？
	// 拓扑 = Exchange（交换机） + Queue（队列） + Binding（绑定关系）
	// 这一步相当于"初始化"RabbitMQ 的基础设施，确保队列和交换机存在

	// 声明 Social 关注模块的拓扑（交换机+队列+绑定）
	if err := declareSocialTopology(ch); err != nil {
		log.Fatalf("Failed to declare social topology: %v", err)
	}

	// 声明 Like 点赞模块的拓扑
	if err := declareLikeTopology(ch); err != nil {
		log.Fatalf("Failed to declare like topology: %v", err)
	}

	// 声明 Comment 评论模块的拓扑
	if err := declareCommentTopology(ch); err != nil {
		log.Fatalf("Failed to declare comment topology: %v", err)
	}

	// 声明 Popularity 热度模块的拓扑（需要 Redis）
	if cache != nil {
		if err := declarePopularityTopology(ch); err != nil {
			log.Fatalf("Failed to declare popularity topology: %v", err)
		}
	}

	// 设置 QoS（服务质量）
	// 参数说明：
	//   50  - 预取消息数量：消费者一次性最多从队列取 50 条消息
	//   0   - 预取大小（字节数）：0 表示不限制
	//   false - 是否应用到所有连接：false 表示只应用到当前通道
	// 作用：防止消息堆积在内存中，实现消息的公平分发
	if err := ch.Qos(50, 0, false); err != nil {
		log.Fatalf("Failed to set qos: %v", err)
	}

	// ========== 4. 创建 Worker 实例 ==========

	// 创建关注 Worker（处理用户关注/取关事件）
	repo := social.NewSocialRepository(sqlDB)
	socialWorker := worker.NewSocialWorker(ch, repo, socialQueue)

	// 创建点赞 Worker（处理点赞/取消点赞事件）
	videoRepo := video.NewVideoRepository(sqlDB)
	likeRepo := video.NewLikeRepository(sqlDB)
	likeWorker := worker.NewLikeWorker(ch, likeRepo, videoRepo, likeQueue)

	// 创建评论 Worker（处理发布/删除评论事件）
	commentRepo := video.NewCommentRepository(sqlDB)
	commentWorker := worker.NewCommentWorker(ch, commentRepo, videoRepo, commentQueue)

	// 创建热度 Worker（处理视频热度更新事件，需要 Redis）
	var popularityWorker *worker.PopularityWorker
	if cache != nil {
		popularityWorker = worker.NewPopularityWorker(ch, cache, popularityQueue)
	}

	// ========== 5. 启动所有 Worker ==========

	// 设置优雅关闭：监听 Ctrl+C 和 SIGTERM 信号
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 错误通道：用于接收 Worker 的错误
	errCh := make(chan error, 4)

	// 启动 Social Worker（并发）
	log.Printf("Worker started, consuming queue=%s", socialQueue)
	go func() { errCh <- socialWorker.Run(ctx) }()

	// 启动 Like Worker（并发）
	log.Printf("Worker started, consuming queue=%s", likeQueue)
	go func() { errCh <- likeWorker.Run(ctx) }()

	// 启动 Comment Worker（并发）
	log.Printf("Worker started, consuming queue=%s", commentQueue)
	go func() { errCh <- commentWorker.Run(ctx) }()

	// 启动 Popularity Worker（并发，如果 Redis 可用）
	if popularityWorker != nil {
		log.Printf("Worker started, consuming queue=%s", popularityQueue)
		go func() { errCh <- popularityWorker.Run(ctx) }()
	}

	// ========== 6. 等待任意一个 Worker 停止 ==========

	// 阻塞等待任意一个 Worker 返回错误
	err = <-errCh
	if err != nil && err != context.Canceled {
		log.Fatalf("Worker stopped: %v", err)
	}
	log.Printf("Worker stopped")
}

// declareSocialTopology 声明 Social 模块的 RabbitMQ 拓扑
// 拓扑 = Exchange + Queue + Binding（交换机 + 队列 + 绑定关系）
//
// 流程图：
//   Producer → Exchange("social.events") → Queue("social.events") → Consumer
//                ↓
//            Routing Key: "social.*"
func declareSocialTopology(ch *amqp.Channel) error {
	// 1. 声明交换机（Exchange）
	// 参数说明：
	//   socialExchange - 交换机名称："social.events"
	//   "topic"        - 交换机类型：Topic 模式（支持通配符匹配）
	//   true           - durable：持久化，RabbitMQ 重启后交换机仍然存在
	//   false          - auto-delete：自动删除，没有队列绑定时是否删除交换机
	//   false          - internal：内部交换机，不允许客户端直接发送消息
	//   false          - no-wait：是否等待服务器响应（true 表示异步）
	//   nil            - arguments：额外参数（如 TTL、死信队列等）
	if err := ch.ExchangeDeclare(
		socialExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	// 2. 声明队列（Queue）
	// 参数说明：
	//   socialQueue - 队列名称："social.events"
	//   true        - durable：持久化，RabbitMQ 重启后队列仍然存在
	//   false       - exclusive：独占队列，仅限当前连接使用
	//   false       - auto-delete：自动删除，没有消费者时是否删除队列
	//   false       - no-wait：是否等待服务器响应
	//   nil         - arguments：额外参数
	// 返回值：q 是队列对象，包含队列名称等信息
	q, err := ch.QueueDeclare(
		socialQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 3. 绑定队列到交换机（Binding）
	// 参数说明：
	//   q.Name          - 队列名称（从 QueueDeclare 返回）
	//   socialBindingKey - 绑定键："social.*"（通配符，匹配所有以 social. 开头的路由键）
	//   socialExchange  - 交换机名称
	//   false           - no-wait：是否等待服务器响应
	//   nil             - arguments：额外参数
	// 作用：告诉交换机，所有 Routing Key 为 "social.*" 的消息都路由到这个队列
	if err := ch.QueueBind(
		q.Name,
		socialBindingKey,
		socialExchange,
		false,
		nil,
	); err != nil {
		return err
	}
	return nil
}

// declarePopularityTopology 声明热度模块的拓扑
// 专门用于处理视频热度更新事件（如点赞+1、评论+1）
func declarePopularityTopology(ch *amqp.Channel) error {
	// 声明热度交换机
	if err := ch.ExchangeDeclare(
		popularityExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	// 声明热度队列
	q, err := ch.QueueDeclare(
		popularityQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 绑定：所有 Routing Key 为 "video.popularity.*" 的消息都路由到这里
	return ch.QueueBind(
		q.Name,
		popularityBindingKey,
		popularityExchange,
		false,
		nil,
	)
}

// declareLikeTopology 声明点赞模块的拓扑
// 处理用户点赞/取消点赞事件
func declareLikeTopology(ch *amqp.Channel) error {
	// 声明点赞交换机
	if err := ch.ExchangeDeclare(
		likeExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	// 声明点赞队列
	q, err := ch.QueueDeclare(
		likeQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 绑定：所有 Routing Key 为 "like.*" 的消息都路由到这里
	return ch.QueueBind(
		q.Name,
		likeBindingKey,
		likeExchange,
		false,
		nil,
	)
}

// declareCommentTopology 声明评论模块的拓扑
// 处理用户发布/删除评论事件
func declareCommentTopology(ch *amqp.Channel) error {
	// 声明评论交换机
	if err := ch.ExchangeDeclare(
		commentExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}

	// 声明评论队列
	q, err := ch.QueueDeclare(
		commentQueue,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// 绑定：所有 Routing Key 为 "comment.*" 的消息都路由到这里
	return ch.QueueBind(
		q.Name,
		commentBindingKey,
		commentExchange,
		false,
		nil,
	)
}
