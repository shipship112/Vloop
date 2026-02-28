package rabbitmq

import (
	"context"
	"errors"
	"time"
)

// PopularityMQ 热度更新消息队列，用于异步更新视频热度
// 热度计算：点赞+1、评论+5、关注+10等，通过MQ异步累积
// Worker消费后会：1) 更新数据库热度值 2) 写入Redis热榜
type PopularityMQ struct {
	*RabbitMQ // 嵌入基础RabbitMQ客户端
}

// 常量定义：交换机、队列、路由键
const (
	popularityExchange   = "video.popularity.events" // 交换机名称
	popularityQueue      = "video.popularity.events" // 队列名称
	popularityBindingKey = "video.popularity.*"     // 绑定键（通配符：匹配所有以video.popularity.开头的路由键）

	popularityUpdateRK = "video.popularity.update" // 热度更新路由键
)

// PopularityEvent 热度更新事件结构体
type PopularityEvent struct {
	EventID    string    `json:"event_id"`   // 事件唯一ID
	VideoID    uint      `json:"video_id"`   // 视频ID
	Change     int64     `json:"change"`     // 热度变化量（可为正数或负数）
	OccurredAt time.Time `json:"occurred_at"` // 事件发生时间
}

// NewPopularityMQ 创建热度更新消息队列实例
// 会声明Topic交换机、队列和绑定关系
// 参数：
//   - base: 基础RabbitMQ客户端
// 返回：
//   - *PopularityMQ: 热度更新消息队列实例
//   - error: 错误信息
func NewPopularityMQ(base *RabbitMQ) (*PopularityMQ, error) {
	if base == nil {
		return nil, errors.New("rabbitmq base is nil")
	}
	// 声明Topic交换机、队列和绑定关系
	if err := base.DeclareTopic(popularityExchange, popularityQueue, popularityBindingKey); err != nil {
		return nil, err
	}
	return &PopularityMQ{RabbitMQ: base}, nil
}

// Update 发送热度更新事件到MQ
// Worker消费后会：1) 更新数据库热度值 2) 写入Redis时间窗ZSET（用于热榜统计）
// 参数：
//   - ctx: 上下文
//   - videoID: 视频ID
//   - change: 热度变化量（例如：点赞+1，评论+5，取消点赞-1）
// 返回：
//   - error: 错误信息
func (p *PopularityMQ) Update(ctx context.Context, videoID uint, change int64) error {
	if p == nil || p.RabbitMQ == nil {
		return errors.New("popularity mq is not initialized")
	}
	if videoID == 0 || change == 0 {
		return errors.New("videoID and change are required")
	}

	// 生成事件ID
	id, err := newEventID(16)
	if err != nil {
		return err
	}

	// 构造热度更新事件
	event := PopularityEvent{
		EventID:    id,
		VideoID:    videoID,
		Change:     change,
		OccurredAt: time.Now().UTC(), // 使用UTC时间
	}

	// 发布事件到MQ
	return p.PublishJSON(ctx, popularityExchange, popularityUpdateRK, event)
}

