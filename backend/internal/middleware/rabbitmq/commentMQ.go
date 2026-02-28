package rabbitmq

import (
	"context"
	"errors"
	"time"
)

// CommentMQ 评论消息队列，用于异步处理评论发布/删除事件
// 工作流程：
// 1. 用户发布/删除评论 → Service层发送事件到MQ
// 2. Worker消费MQ消息 → 更新数据库（插入/删除评论记录、更新评论数）
type CommentMQ struct {
	*RabbitMQ // 嵌入基础RabbitMQ客户端
}

// 常量定义：交换机、队列、路由键
const (
	commentExchange   = "comment.events" // 交换机名称
	commentQueue      = "comment.events" // 队列名称
	commentBindingKey = "comment.*"     // 绑定键（通配符：匹配所有以comment.开头的路由键）

	commentPublishRK = "comment.publish" // 发布评论路由键
	commentDeleteRK  = "comment.delete"  // 删除评论路由键
)

// CommentEvent 评论事件结构体
type CommentEvent struct {
	EventID    string    `json:"event_id"`             // 事件唯一ID
	Action     string    `json:"action"`              // 操作类型：publish/delete
	CommentID  uint      `json:"comment_id,omitempty"`  // 评论ID（删除时使用）
	Username   string    `json:"username,omitempty"`   // 用户名（发布时使用）
	VideoID    uint      `json:"video_id,omitempty"`   // 视频ID（发布时使用）
	AuthorID   uint      `json:"author_id,omitempty"`  // 作者ID（发布时使用）
	Content    string    `json:"content,omitempty"`    // 评论内容（发布时使用）
	OccurredAt time.Time `json:"occurred_at"`         // 事件发生时间
}

// NewCommentMQ 创建评论消息队列实例
// 会声明Topic交换机、队列和绑定关系
// 参数：
//   - base: 基础RabbitMQ客户端
// 返回：
//   - *CommentMQ: 评论消息队列实例
//   - error: 错误信息
func NewCommentMQ(base *RabbitMQ) (*CommentMQ, error) {
	if base == nil {
		return nil, errors.New("rabbitmq base is nil")
	}
	// 声明Topic交换机、队列和绑定关系
	if err := base.DeclareTopic(commentExchange, commentQueue, commentBindingKey); err != nil {
		return nil, err
	}
	return &CommentMQ{RabbitMQ: base}, nil
}

// Publish 发送发布评论事件到MQ
// Worker消费后会：1) 插入评论记录 2) 视频热度+5
// 参数：
//   - ctx: 上下文
//   - username: 用户名
//   - videoID: 视频ID
//   - authorID: 作者ID
//   - content: 评论内容
// 返回：
//   - error: 错误信息
func (c *CommentMQ) Publish(ctx context.Context, username string, videoID, authorID uint, content string) error {
	return c.publish(ctx, "publish", commentPublishRK, CommentEvent{
		Username: username,
		VideoID:  videoID,
		AuthorID: authorID,
		Content:  content,
	})
}

// Delete 发送删除评论事件到MQ
// Worker消费后会：1) 删除评论记录 2) 视频热度-5
// 参数：
//   - ctx: 上下文
//   - commentID: 评论ID
// 返回：
//   - error: 错误信息
func (c *CommentMQ) Delete(ctx context.Context, commentID uint) error {
	return c.publish(ctx, "delete", commentDeleteRK, CommentEvent{
		CommentID: commentID,
	})
}

// publish 发送评论事件到MQ（内部方法）
// 参数：
//   - ctx: 上下文
//   - action: 操作类型（publish/delete）
//   - routingKey: 路由键
//   - evt: 评论事件
// 返回：
//   - error: 错误信息
func (c *CommentMQ) publish(ctx context.Context, action, routingKey string, evt CommentEvent) error {
	if c == nil || c.RabbitMQ == nil {
		return errors.New("comment mq is not initialized")
	}

	// 生成事件ID
	id, err := newEventID(16)
	if err != nil {
		return err
	}

	// 填充事件字段
	evt.EventID = id
	evt.Action = action
	evt.OccurredAt = time.Now().UTC()

	// 发布事件到MQ
	return c.PublishJSON(ctx, commentExchange, routingKey, evt)
}

