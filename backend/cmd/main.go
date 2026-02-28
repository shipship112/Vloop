// Package main 是 Web 服务器（API 服务器）的入口程序
// 与 worker 程序不同，main.go 负责：
//   1. 启动 HTTP 服务器（处理用户请求）
//   2. 发送消息到 MQ（作为生产者 Producer）
//
// worker/main.go 负责：
//   1. 消费 MQ 消息（作为消费者 Consumer）
//   2. 异步处理业务逻辑（更新数据库、Redis 等）
package main

import (
	"context"
	"feedsystem_video_go/internal/config"
	"feedsystem_video_go/internal/db"
	apphttp "feedsystem_video_go/internal/http"
	rabbitmq "feedsystem_video_go/internal/middleware/rabbitmq"
	rediscache "feedsystem_video_go/internal/middleware/redis"
	"log"
	"strconv"
	"time"
)

func main() {
	// ========== 1. 加载配置 ==========
	log.Printf("Loading config from configs/config.yaml")
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// ========== 2. 连接数据库 ==========
	sqlDB, err := db.NewDB(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect database: %v", err)
	}
	// 自动迁移：根据 GORM 模型创建/更新数据库表结构
	if err := db.AutoMigrate(sqlDB); err != nil {
		log.Fatalf("Failed to auto migrate database: %v", err)
	}
	defer db.CloseDB(sqlDB)

	// ========== 3. 连接 Redis（可选，用于缓存） ==========
	// 如果 Redis 不可用，缓存功能会被禁用，但程序仍可运行
	cache, err := rediscache.NewFromEnv(&cfg.Redis)
	if err != nil {
		log.Printf("Redis config error (cache disabled): %v", err)
		cache = nil
	} else {
		pingCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if err := cache.Ping(pingCtx); err != nil {
			log.Printf("Redis not available (cache disabled): %v", err)
			_ = cache.Close()
			cache = nil
		} else {
			defer cache.Close()
			log.Printf("Redis connected (cache enabled)")
		}
	}

	// ========== 4. 连接 RabbitMQ（可选，用于消息队列） ==========
	// 注意：main.go 作为生产者（Producer），只负责发送消息
	// worker/main.go 作为消费者（Consumer），负责消费消息
	//
	// 如果 RabbitMQ 不可用，MQ 功能会被禁用，Service 层会使用 Fallback 降级机制
	// （直接写数据库，不经过 MQ）
	rmq, err := rabbitmq.NewRabbitMQ(&cfg.RabbitMQ)
	if err != nil {
		log.Printf("RabbitMQ config error (disabled): %v", err)
		rmq = nil
	} else {
		defer rmq.Close()
		log.Printf("RabbitMQ connected")
	}

	// ========== 5. 设置路由并启动服务器 ==========
	// SetRouter 会初始化所有模块的 Service，并把 RMQ 注入进去
	// 这样 Service 就可以通过 MQ 发送消息了
	r := apphttp.SetRouter(sqlDB, cache, rmq)
	log.Printf("Server is running on port %d", cfg.Server.Port)
	if err := r.Run(":" + strconv.Itoa(cfg.Server.Port)); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}
