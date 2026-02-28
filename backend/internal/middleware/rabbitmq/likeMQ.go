package rabbitmq

import (
	"context"
	"errors"
	"time"
)

// LikeMQ 点赞消息队列，用于异步处理点赞/取消点赞事件
// 工作流程：
// 1. 用户点赞 → Service层发送点赞事件到MQ
// 2. Worker消费MQ消息 → 更新数据库（插入/删除点赞记录、更新点赞数）
type LikeMQ struct {
	*RabbitMQ // 嵌入基础RabbitMQ客户端
}

// 常量定义：交换机、队列、路由键
const (
	likeExchange   = "like.events"  // 交换机名称
	likeQueue      = "like.events"  // 队列名称
	likeBindingKey = "like.*"      // 绑定键（通配符：匹配所有以like.开头的路由键）

	likeLikeRK   = "like.like"     // 点赞路由键
	likeUnlikeRK = "like.unlike"   // 取消点赞路由键
)

// LikeEvent 点赞事件结构体
type LikeEvent struct {
	EventID    string    `json:"event_id"`   // 事件唯一ID
	Action     string    `json:"action"`     // 操作类型：like/unlike
	UserID     uint      `json:"user_id"`    // 用户ID
	VideoID    uint      `json:"video_id"`   // 视频ID
	OccurredAt time.Time `json:"occurred_at"` // 事件发生时间
}

// NewLikeMQ 创建点赞消息队列实例
// 会声明Topic交换机、队列和绑定关系
// 参数：
//   - base: 基础RabbitMQ客户端
// 返回：
//   - *LikeMQ: 点赞消息队列实例
//   - error: 错误信息
func NewLikeMQ(base *RabbitMQ) (*LikeMQ, error) {
	if base == nil {
		return nil, errors.New("rabbitmq base is nil")
	}
	// 声明Topic交换机、队列和绑定关系
	if err := base.DeclareTopic(likeExchange, likeQueue, likeBindingKey); err != nil {
		return nil, err
	}
	return &LikeMQ{RabbitMQ: base}, nil
}

// Like 发送点赞事件到MQ
// Worker消费后会：1) 插入点赞记录 2) 视频点赞数+1 3) 视频热度+1
// 参数：
//   - ctx: 上下文
//   - userID: 用户ID
//   - videoID: 视频ID
// 返回：
//   - error: 错误信息
func (l *LikeMQ) Like(ctx context.Context, userID, videoID uint) error {
	return l.publish(ctx, "like", likeLikeRK, userID, videoID)
}

// Unlike 发送取消点赞事件到MQ
// Worker消费后会：1) 删除点赞记录 2) 视频点赞数-1 3) 视频热度-1
// 参数：
//   - ctx: 上下文
//   - userID: 用户ID
//   - videoID: 视频ID
// 返回：
//   - error: 错误信息
func (l *LikeMQ) Unlike(ctx context.Context, userID, videoID uint) error {
	return l.publish(ctx, "unlike", likeUnlikeRK, userID, videoID)
}

// publish 发送点赞事件到MQ（内部方法）
// 参数：
//   - ctx: 上下文
//   - action: 操作类型（like/unlike）
//   - routingKey: 路由键
//   - userID: 用户ID
//   - videoID: 视频ID
// 返回：
//   - error: 错误信息
func (l *LikeMQ) publish(ctx context.Context, action, routingKey string, userID, videoID uint) error {
	if l == nil || l.RabbitMQ == nil {
		return errors.New("like mq is not initialized")
	}
	if userID == 0 || videoID == 0 {
		return errors.New("userID and videoID are required")
	}

	// 生成事件ID
	id, err := newEventID(16)
	if err != nil {
		return err
	}

	// 构造点赞事件
	event := LikeEvent{
		EventID:    id,
		Action:     action,
		UserID:     userID,
		VideoID:    videoID,
		OccurredAt: time.Now(),
	}

	// 发布事件到MQ
	return l.PublishJSON(ctx, likeExchange, routingKey, event)
}
