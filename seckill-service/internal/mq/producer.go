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

const (
	reconnectBaseWait = time.Second      // 重连基础等待时间
	reconnectMaxWait  = 30 * time.Second // 重连最大等待时间
)

// SeckillOrderMessage 秒杀订单消息
type SeckillOrderMessage struct {
	OrderId          string `json:"order_id"`
	UserId           int64  `json:"user_id"`
	SeckillProductId int64  `json:"seckill_product_id"`
	ProductId        int64  `json:"product_id"`
	Quantity         int64  `json:"quantity"`
	Amount           int64  `json:"amount"`
	SeckillPrice     int64  `json:"seckill_price"`
	CreatedAt        int64  `json:"created_at"`
}

// Producer RabbitMQ 生产者
type Producer struct {
	conn       *amqp091.Connection
	channel    *amqp091.Channel
	exchange   string
	routingKey string
	mu         sync.Mutex
	url        string // 保存连接串，重连复用
}

// NewProducer 创建 RabbitMQ 生产者
func NewProducer(url, exchange, routingKey string) (*Producer, error) {
	p := &Producer{
		url:        url,
		exchange:   exchange,
		routingKey: routingKey,
	}
	if err := p.connect(); err != nil {
		return nil, err
	}
	p.monitorConnection()
	logx.Infof("RabbitMQ producer created: exchange=%s, routingKey=%s", exchange, routingKey)
	return p, nil
}

// connect 建立连接、开 channel、声明交换机
func (p *Producer) connect() error {
	conn, err := amqp091.Dial(p.url)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	if err = ch.ExchangeDeclare(p.exchange, ExchangeTypeDirect, true, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	p.conn = conn
	p.channel = ch
	return nil
}

// reconnect 关闭旧连接，重建连接（调用方持锁）
func (p *Producer) reconnect() error {
	if p.channel != nil {
		p.channel.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
	if err := p.connect(); err != nil {
		return err
	}
	p.monitorConnection()
	logx.Infof("RabbitMQ producer reconnected")
	return nil
}

// monitorConnection 监听连接断开事件，断开后指数退避自动重连
func (p *Producer) monitorConnection() {
	notify := p.conn.NotifyClose(make(chan *amqp091.Error, 1))
	go func() {
		amqpErr := <-notify
		if amqpErr == nil {
			return // 正常关闭（Close() 调用），不重连
		}
		logx.Errorf("Producer 连接断开: %v，开始自动重连...", amqpErr)

		backoff := reconnectBaseWait
		for attempt := 1; ; attempt++ {
			time.Sleep(backoff)
			p.mu.Lock()
			err := p.reconnect()
			p.mu.Unlock()
			if err == nil {
				logx.Infof("Producer 重连成功（第 %d 次）", attempt)
				return
			}
			logx.Errorf("Producer 重连失败（第 %d 次）: %v", attempt, err)
			if backoff < reconnectMaxWait {
				backoff *= 2
			}
		}
	}()
}

// publish 内部发布方法（调用方持锁）
func (p *Producer) publish(ctx context.Context, routingKey string, body []byte) error {
	return p.channel.PublishWithContext(ctx, p.exchange, routingKey, false, false,
		amqp091.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp091.Persistent,
			Timestamp:    time.Now(),
			Body:         body,
		},
	)
}

// SendSeckillOrder 发送秒杀订单消息
func (p *Producer) SendSeckillOrder(ctx context.Context, msg *SeckillOrderMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化秒杀消息失败: %w", err)
	}
	if err = p.publish(ctx, p.routingKey, body); err != nil {
		logx.Errorf("发送秒杀消息失败: orderId=%s, err=%v", msg.OrderId, err)
		return fmt.Errorf("发送消息失败: %w", err)
	}
	logx.Infof("秒杀消息发送成功: orderId=%s", msg.OrderId)
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
	if err = p.publish(ctx, SeckillDelayRoutingKey, body); err != nil {
		logx.Errorf("发送延迟消息失败: orderId=%s, err=%v", msg.OrderId, err)
		return fmt.Errorf("发送延迟消息失败: %w", err)
	}
	logx.Infof("延迟兜底消息发送成功: orderId=%s", msg.OrderId)
	return nil
}

// Close 关闭生产者（正常关闭，不触发重连）
func (p *Producer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel != nil {
		p.channel.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
	return nil
}
