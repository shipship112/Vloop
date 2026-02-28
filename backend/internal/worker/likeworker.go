// Package worker 定义了 RabbitMQ 消费者（Worker）
// Worker 的作用：作为后台服务，持续监听 RabbitMQ 队列，获取消息并异步处理业务逻辑
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"feedsystem_video_go/internal/middleware/rabbitmq"
	"feedsystem_video_go/internal/video"
	"log"
	amqp "github.com/rabbitmq/amqp091-go"
	"time"
)

// LikeWorker 点赞事件消费者
// 职责：从队列中获取点赞消息，更新数据库（点赞表 + 视频点赞数 + 视频热度）
type LikeWorker struct {
	ch     *amqp.Channel         // RabbitMQ 通道，用于消费消息
	likes  *video.LikeRepository // 点赞数据访问层，操作点赞表
	videos *video.VideoRepository // 视频数据访问层，更新点赞数和热度
	queue  string                 // 队列名称，监听哪个队列
}

// NewLikeWorker 创建点赞 Worker 实例
// 参数：
//   ch - RabbitMQ 通道
//   likes - 点赞仓储（操作数据库）
//   videos - 视频仓储（更新点赞数）
//   queue - 队列名称
func NewLikeWorker(ch *amqp.Channel, likes *video.LikeRepository, videos *video.VideoRepository, queue string) *LikeWorker {
	return &LikeWorker{ch: ch, likes: likes, videos: videos, queue: queue}
}

// Run 启动 Worker，开始消费消息
// 这是一个**阻塞方法**，会一直运行直到收到取消信号
//
// 工作流程：
//   1. 注册消费者到 RabbitMQ 队列
//   2. RabbitMQ 推送消息到 deliveries 通道
//   3. 遍历 deliveries 通道，处理每条消息
//   4. 处理完成后发送 ACK（确认）或 NACK（拒绝）
//
// 参数：
//   ctx - 上下文，用于优雅关闭（收到中断信号时取消）
//
// 返回：
//   error - 错误信息（通常只有当需要停止时才返回）
func (w *LikeWorker) Run(ctx context.Context) error {
	// ========== 1. 参数校验 ==========
	if w == nil || w.ch == nil || w.likes == nil || w.videos == nil {
		return errors.New("like worker is not initialized")
	}
	if w.queue == "" {
		return errors.New("queue is required")
	}

	// ========== 2. 注册消费者 ==========

	// Consume：向 RabbitMQ 注册消费者，开始消费队列中的消息
	// 参数说明：
	//   w.queue - 队列名称
	//   ""      - 消费者标签，空字符串表示自动生成
	//   false   - auto-ack：是否自动确认消息（false 表示手动确认）
	//   false   - exclusive：独占模式，仅此消费者可以消费该队列
	//   false   - no-local：不允许接收本连接发布的消息
	//   false   - no-wait：是否等待服务器响应
	//   nil     - arguments：额外参数
	// 返回值：deliveries 是消息通道，RabbitMQ 会把消息推送到这个通道
	deliveries, err := w.ch.Consume(
		w.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	// ========== 3. 消息消费循环 ==========

	for {
		select {
		// 收到取消信号（如 Ctrl+C），退出循环
		case <-ctx.Done():
			return ctx.Err()

		// 从 RabbitMQ 接收消息
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries channel closed")
			}
			// 处理消息（包括 ACK/NACK）
			w.handleDelivery(ctx, d)
		}
	}
}

// handleDelivery 处理单条消息
// 职责：处理消息 → 发送 ACK/NACK（确认或拒绝）
//
// 为什么需要 ACK/NACK？
//   - ACK（Acknowledge）：告诉 RabbitMQ"消息处理成功"，消息从队列删除
//   - NACK（Negative Acknowledge）：告诉 RabbitMQ"消息处理失败"，消息重新入队
//
// 参数：
//   ctx - 上下文
//   d - 消息对象（包含消息体、元数据等）
func (w *LikeWorker) handleDelivery(ctx context.Context, d amqp.Delivery) {
	// 尝试处理消息
	if err := w.process(ctx, d.Body); err != nil {
		// 处理失败，发送 NACK
		// 参数说明：
		//   false - multiple：是否批量拒绝（false 表示只拒绝当前消息）
		//   true  - requeue：是否重新入队（true 表示消息重新放回队列，下次再消费）
		log.Printf("like worker: failed to process message: %v", err)
		_ = d.Nack(false, true)
		return
	}

	// 处理成功，发送 ACK
	// 参数说明：false - multiple：是否批量确认（false 表示只确认当前消息）
	// 注意：消息被确认后，RabbitMQ 会从队列中删除它
	_ = d.Ack(false)
}

// process 解析并处理消息体
// 业务逻辑流程：
//   1. 反序列化 JSON 消息体 → LikeEvent 结构体
//   2. 参数校验（用户ID和视频ID必须有效）
//   3. 根据 Action 字段分发处理（like/unlike）
//
// 参数：
//   ctx - 上下文
//   body - 消息体（JSON 字节数组）
//
// 返回：
//   error - 处理错误（nil 表示成功）
func (w *LikeWorker) process(ctx context.Context, body []byte) error {
	// 1. 反序列化 JSON 消息体
	var evt rabbitmq.LikeEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		// 解析事件失败（可能是消息格式错误），直接丢弃
		// 返回 nil 而不是 error，因为格式错误的消息不应该重新入队
		return nil
	}

	// 2. 参数校验：用户ID和视频ID必须有效
	if evt.UserID == 0 || evt.VideoID == 0 {
		return nil
	}

	// 3. 根据 Action 字段分发处理
	// Action 可能的值：
	//   "like" - 点赞
	//   "unlike" - 取消点赞
	switch evt.Action {
	case "like":
		return w.applyLike(ctx, evt.UserID, evt.VideoID)
	case "unlike":
		return w.applyUnlike(ctx, evt.UserID, evt.VideoID)
	default:
		// 未知的 Action，忽略
		return nil
	}
}

// applyLike 执行点赞业务逻辑
// 数据库操作：
//   1. 检查视频是否存在（防止给不存在的视频点赞）
//   2. 插入点赞记录（忽略重复点赞）
//   3. 更新视频点赞数（+1）
//   4. 更新视频热度（+1）
//
// 参数：
//   ctx - 上下文
//   userID - 点赞用户的 ID
//   videoID - 被点赞视频的 ID
//
// 返回：
//   error - 操作错误
func (w *LikeWorker) applyLike(ctx context.Context, userID, videoID uint) error {
	// 1. 检查视频是否存在
	// 场景：视频可能在点赞前被删除了，需要防御性检查
	ok, err := w.videos.IsExist(ctx, videoID)
	if err != nil {
		return err
	}
	if !ok {
		// 视频不存在，直接返回（不需要报错，因为这是合法的边界情况）
		return nil
	}

	// 2. 插入点赞记录（忽略重复点赞）
	// 为什么需要忽略重复？因为同一个用户可能多次点击"点赞"按钮
	// 数据库有唯一索引（video_id + account_id），重复插入会失败
	// LikeIgnoreDuplicate 方法会忽略重复错误
	created, err := w.likes.LikeIgnoreDuplicate(ctx, &video.Like{
		VideoID:   videoID,
		AccountID: userID,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	if !created {
		// 没有创建记录（可能是重复点赞），直接返回
		return nil
	}

	// 3. 更新视频点赞数（+1）
	if err := w.videos.ChangeLikesCount(ctx, videoID, 1); err != nil {
		return err
	}

	// 4. 更新视频热度（+1）
	// 热度计算规则：点赞+1，评论+1，关注+10
	return w.videos.ChangePopularity(ctx, videoID, 1)
}

// applyUnlike 执行取消点赞业务逻辑
// 数据库操作：
//   1. 检查视频是否存在
//   2. 删除点赞记录
//   3. 更新视频点赞数（-1）
//   4. 更新视频热度（-1）
//
// 参数：
//   ctx - 上下文
//   userID - 取消点赞用户的 ID
//   videoID - 被取消点赞视频的 ID
//
// 返回：
//   error - 操作错误
func (w *LikeWorker) applyUnlike(ctx context.Context, userID, videoID uint) error {
	// 1. 检查视频是否存在
	ok, err := w.videos.IsExist(ctx, videoID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	// 2. 删除点赞记录
	// DeleteByVideoAndAccount 返回是否删除成功
	deleted, err := w.likes.DeleteByVideoAndAccount(ctx, videoID, userID)
	if err != nil {
		return err
	}
	if !deleted {
		// 没有删除记录（可能本来就没点赞），直接返回
		return nil
	}

	// 3. 更新视频点赞数（-1）
	if err := w.videos.ChangeLikesCount(ctx, videoID, -1); err != nil {
		return err
	}

	// 4. 更新视频热度（-1）
	return w.videos.ChangePopularity(ctx, videoID, -1)
}
