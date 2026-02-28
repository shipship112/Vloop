package rabbitmq

import (
	"context"
	"errors"
	"time"
)

// SocialMQ 关注消息队列，用于异步处理关注/取关事件
// 工作流程：
// 1. 用户关注/取关 → Service层发送事件到MQ
// 2. Worker消费MQ消息 → 更新数据库（插入/删除关注记录、更新粉丝数、更新视频热度）
type SocialMQ struct {
	*RabbitMQ // 嵌入基础RabbitMQ客户端
}

// 常量定义：交换机、队列、路由键
const (
	socialExchange   = "social.events" // 交换机名称
	socialQueue      = "social.events" // 队列名称
	socialBindingKey = "social.*"     // 绑定键（通配符：匹配所有以social.开头的路由键）

	socialFollowRK   = "social.follow"   // 关注路由键
	socialUnfollowRK = "social.unfollow" // 取关路由键
)

// SocialEvent 关注事件结构体
type SocialEvent struct {
	EventID    string    `json:"event_id"`   // 事件唯一ID
	Action     string    `json:"action"`     // 操作类型：follow/unfollow
	FollowerID uint      `json:"follower_id"` // 关注者ID
	VloggerID  uint      `json:"vlogger_id"`  // 被关注者（博主）ID
	OccurredAt time.Time `json:"occurred_at"` // 事件发生时间
}

// NewSocialMQ 创建关注消息队列实例
// 会声明Topic交换机、队列和绑定关系
// 参数：
//   - base: 基础RabbitMQ客户端
// 返回：
//   - *SocialMQ: 关注消息队列实例
//   - error: 错误信息
func NewSocialMQ(base *RabbitMQ) (*SocialMQ, error) {
	if base == nil {
		return nil, errors.New("rabbitmq base is nil")
	}
	// 声明Topic交换机、队列和绑定关系
	if err := base.DeclareTopic(socialExchange, socialQueue, socialBindingKey); err != nil {
		return nil, err
	}
	return &SocialMQ{RabbitMQ: base}, nil
}

// Follow 发送关注事件到MQ
// Worker消费后会：1) 插入关注记录 2) 被关注者粉丝数+1 3) 被关注者最新视频热度+10
// 参数：
//   - ctx: 上下文
//   - followerID: 关注者ID
//   - vloggerID: 被关注者（博主）ID
// 返回：
//   - error: 错误信息
func (s *SocialMQ) Follow(ctx context.Context, followerID, vloggerID uint) error {
	return s.publish(ctx, "follow", socialFollowRK, followerID, vloggerID)
}

// UnFollow 发送取关事件到MQ
// Worker消费后会：1) 删除关注记录 2) 被关注者粉丝数-1 3) 被关注者最新视频热度-10
// 参数：
//   - ctx: 上下文
//   - followerID: 关注者ID
//   - vloggerID: 被关注者（博主）ID
// 返回：
//   - error: 错误信息
func (s *SocialMQ) UnFollow(ctx context.Context, followerID, vloggerID uint) error {
	return s.publish(ctx, "unfollow", socialUnfollowRK, followerID, vloggerID)
}

// publish 发送关注事件到MQ（内部方法）
// 参数：
//   - ctx: 上下文
//   - action: 操作类型（follow/unfollow）
//   - routingKey: 路由键
//   - followerID: 关注者ID
//   - vloggerID: 被关注者（博主）ID
// 返回：
//   - error: 错误信息
func (s *SocialMQ) publish(ctx context.Context, action, routingKey string, followerID, vloggerID uint) error {
	if s == nil || s.RabbitMQ == nil {
		return errors.New("social mq is not initialized")
	}
	if followerID == 0 || vloggerID == 0 {
		return errors.New("followerID and vloggerID are required")
	}

	// 生成事件ID
	id, err := newEventID(16)
	if err != nil {
		return err
	}

	// 构造关注事件
	evt := SocialEvent{
		EventID:    id,
		Action:     action,
		FollowerID: followerID,
		VloggerID:  vloggerID,
		OccurredAt: time.Now().UTC(),
	}

	// 发布事件到MQ
	return s.PublishJSON(ctx, socialExchange, routingKey, evt)
}
