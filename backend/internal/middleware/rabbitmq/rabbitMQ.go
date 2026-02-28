package rabbitmq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"feedsystem_video_go/internal/config"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ RabbitMQ客户端封装
// RabbitMQ是一个消息队列中间件，用于实现异步通信和解耦
// 核心概念：
//   - Exchange（交换机）：接收消息并路由到队列
//   - Queue（队列）：存储消息，消费者从这里获取消息
//   - Routing Key（路由键）：用于决定消息路由到哪个队列
//   - Binding（绑定）：将队列绑定到交换机，并指定路由键
type RabbitMQ struct {
	conn *amqp.Connection // RabbitMQ连接
	ch   *amqp.Channel    // RabbitMQ通道（轻量级连接，用于发送和接收消息）
}

// NewRabbitMQ 创建RabbitMQ连接和通道
// 参数：
//   - cfg: RabbitMQ配置（用户名、密码、主机、端口）
// 返回：
//   - *RabbitMQ: RabbitMQ客户端实例
//   - error: 错误信息
func NewRabbitMQ(cfg *config.RabbitMQConfig) (*RabbitMQ, error) {
	if cfg == nil {
		return nil, errors.New("rabbitmq config is nil")
	}
	// 构造连接URL：amqp://用户名:密码@主机:端口/
	url := "amqp://" + cfg.Username + ":" + cfg.Password + "@" + cfg.Host + ":" + strconv.Itoa(cfg.Port) + "/"

	// 建立RabbitMQ连接
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}

	// 创建通道（Channel是轻量级连接，一个连接可以创建多个通道）
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	return &RabbitMQ{conn: conn, ch: ch}, nil
}

// Close 关闭RabbitMQ连接和通道
// 应该在程序退出时调用，释放资源
func (r *RabbitMQ) Close() error {
	if r == nil || r.ch == nil || r.conn == nil {
		return nil
	}
	// 先关闭通道
	if err := r.ch.Close(); err != nil {
		return err
	}
	// 再关闭连接
	if err := r.conn.Close(); err != nil {
		return err
	}
	return nil
}

// DeclareTopic 声明Topic类型的交换机、队列和绑定关系
// Topic交换机：根据路由键的通配符匹配来路由消息
// 例如：
//   - 路由键 "like.like" 可以匹配绑定键 "like.*"
//   - 路由键 "video.popularity.update" 可以匹配绑定键 "video.popularity.*"
// 参数：
//   - exchange: 交换机名称
//   - queue: 队列名称
//   - bindingKey: 绑定键（支持通配符 * 和 #）
// 返回：
//   - error: 错误信息
func (r *RabbitMQ) DeclareTopic(exchange string, queue string, bindingKey string) error {
	if r == nil || r.ch == nil {
		return errors.New("rabbitmq is not initialized")
	}
	if exchange == "" || queue == "" || bindingKey == "" {
		return errors.New("exchange/queue/bindingKey is required")
	}

	// 1. 声明交换机（Topic类型，持久化）
	if err := r.ch.ExchangeDeclare(
		exchange,       // 交换机名称
		"topic",        // 交换机类型（topic支持通配符路由）
		true,           // durable: 持久化（RabbitMQ重启后仍存在）
		false,          // autoDelete: 不自动删除
		false,          // internal: 不使用内部交换机
		false,          // noWait: 不等待服务器确认
		nil,            // args: 额外参数
	); err != nil {
		return err
	}

	// 2. 声明队列（持久化）
	q, err := r.ch.QueueDeclare(
		queue,          // 队列名称
		true,           // durable: 持久化
		false,          // autoDelete: 不自动删除
		false,          // exclusive: 不独占
		false,          // noWait: 不等待服务器确认
		nil,            // args: 额外参数
	)
	if err != nil {
		return err
	}

	// 3. 将队列绑定到交换机（通过绑定键）
	return r.ch.QueueBind(
		q.Name,         // 队列名称
		bindingKey,     // 绑定键（支持通配符）
		exchange,       // 交换机名称
		false,          // noWait: 不等待服务器确认
		nil,            // args: 额外参数
	)
}

// PublishJSON 发布JSON格式消息到指定的交换机
// 参数：
//   - ctx: 上下文（用于超时控制）
//   - exchange: 交换机名称
//   - routingKey: 路由键（决定消息路由到哪个队列）
//   - payload: 消息内容（任意对象，会被序列化为JSON）
// 返回：
//   - error: 错误信息
func (r *RabbitMQ) PublishJSON(ctx context.Context, exchange string, routingKey string, payload any) error {
	if r == nil || r.ch == nil {
		return errors.New("rabbitmq is not initialized")
	}
	if exchange == "" || routingKey == "" {
		return errors.New("exchange and routingKey are required")
	}

	// 将payload序列化为JSON
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// 发布消息到交换机
	return r.ch.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json", // 内容类型
		DeliveryMode: amqp.Persistent,    // 持久化模式（RabbitMQ重启后消息不丢失）
		Timestamp:    time.Now(),         // 消息时间戳
		Body:         b,                  // 消息体（JSON字节）
	})
}

// newEventID 生成随机事件ID（16字节=32位十六进制字符串）
// 用于标识每个事件的唯一性
func newEventID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
