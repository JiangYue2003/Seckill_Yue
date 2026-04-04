package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/zeromicro/go-zero/core/logx"
)

// Exchange 定义交换机类型
const (
	ExchangeTypeDirect = "direct"
	ExchangeTypeFanout = "fanout"
	ExchangeTypeTopic  = "topic"
)

// RoutingKey 路由键
const (
	SeckillOrderRoutingKey = "seckill.order"
	SeckillDelayRoutingKey = "seckill.delay" // 延迟队列路由键
)

// SeckillOrderMessage 秒杀订单消息
type SeckillOrderMessage struct {
	OrderId          string `json:"order_id"`           // 订单号
	UserId           int64  `json:"user_id"`            // 用户ID
	SeckillProductId int64  `json:"seckill_product_id"` // 秒杀商品ID
	ProductId        int64  `json:"product_id"`         // 真实商品ID
	Quantity         int64  `json:"quantity"`           // 购买数量
	Amount           int64  `json:"amount"`             // 实付金额(分)
	SeckillPrice     int64  `json:"seckill_price"`      // 秒杀价格(分)
	CreatedAt        int64  `json:"created_at"`         // 创建时间戳
}

// Producer RabbitMQ 生产者
type Producer struct {
	conn       *amqp091.Connection
	channel    *amqp091.Channel
	exchange   string
	routingKey string
	mu         sync.Mutex
}

// NewProducer 创建 RabbitMQ 生产者
func NewProducer(url, exchange, routingKey string) (*Producer, error) {
	// 建立连接
	conn, err := amqp091.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	// 创建通道
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// 声明交换机
	err = ch.ExchangeDeclare(
		exchange,           // 交换机名称
		ExchangeTypeDirect, // 类型
		true,               // durable - 持久化
		false,              // auto-deleted
		false,              // internal
		false,              // no-wait
		nil,                // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	logx.Infof("RabbitMQ producer created: exchange=%s, routingKey=%s", exchange, routingKey)

	return &Producer{
		conn:       conn,
		channel:    ch,
		exchange:   exchange,
		routingKey: routingKey,
	}, nil
}

// SendSeckillOrder 发送秒杀订单消息
func (p *Producer) SendSeckillOrder(ctx context.Context, msg *SeckillOrderMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 序列化消息
	body, err := json.Marshal(msg)
	if err != nil {
		logx.Errorf("序列化秒杀消息失败: %v", err)
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	// 发送消息到交换机
	err = p.channel.PublishWithContext(
		ctx,
		p.exchange,   // 交换机
		p.routingKey, // 路由键
		false,        // mandatory
		false,        // immediate
		amqp091.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp091.Persistent, // 持久化
			Timestamp:    time.Now(),
			Body:         body,
		},
	)
	if err != nil {
		logx.Errorf("发送秒杀消息失败: orderId=%s, err=%v", msg.OrderId, err)
		return fmt.Errorf("发送消息失败: %w", err)
	}

	logx.Infof("秒杀消息发送成功: orderId=%s, routingKey=%s", msg.OrderId, p.routingKey)
	return nil
}

// SendDelayOrder 发送延迟兜底消息到延迟队列（5分钟后路由到超时检查队列）
func (p *Producer) SendDelayOrder(ctx context.Context, msg *SeckillOrderMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化延迟消息失败: %w", err)
	}

	err = p.channel.PublishWithContext(
		ctx,
		p.exchange,             // 同一主交换机
		SeckillDelayRoutingKey, // 路由到延迟队列
		false,
		false,
		amqp091.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp091.Persistent,
			Timestamp:    time.Now(),
			Body:         body,
		},
	)
	if err != nil {
		logx.Errorf("发送延迟消息失败: orderId=%s, err=%v", msg.OrderId, err)
		return fmt.Errorf("发送延迟消息失败: %w", err)
	}

	logx.Infof("延迟兜底消息发送成功: orderId=%s, delay=5min", msg.OrderId)
	return nil
}

// Close 关闭生产者
func (p *Producer) Close() error {
	if p.channel != nil {
		p.channel.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
	return nil
}
