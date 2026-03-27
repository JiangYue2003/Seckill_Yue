package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/rabbitmq/amqp091-go"
	"github.com/zeromicro/go-zero/core/logx"
)

// SeckillOrderQueueName 秒杀订单队列名称
const SeckillOrderQueueName = "seckill_order_queue"

// SeckillOrderMessage 秒杀成功消息
type SeckillOrderMessage struct {
	OrderId          string `json:"order_id"`           // 订单号
	UserId           int64  `json:"user_id"`            // 用户ID
	SeckillProductId int64  `json:"seckill_product_id"` // 秒杀商品ID
	ProductId        int64  `json:"product_id"`         // 真实商品ID
	Quantity         int64  `json:"quantity"`           // 购买数量
	SeckillPrice     int64  `json:"seckill_price"`      // 秒杀价格(分)
	Amount           int64  `json:"amount"`             // 实付金额(分)
	CreatedAt        int64  `json:"created_at"`         // 创建时间戳
}

// ProcessFunc 消息处理函数类型
type ProcessFunc func(msg *SeckillOrderMessage) error

// Consumer RabbitMQ 消费者
type Consumer struct {
	conn        *amqp091.Connection
	channel     *amqp091.Channel
	exchange    string
	routingKey  string
	queueName   string
	processFunc ProcessFunc
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	done        chan struct{}
}

// NewConsumer 创建 RabbitMQ 消费者
func NewConsumer(url, exchange, routingKey, queueName, consumerTag string, processFunc ProcessFunc) (*Consumer, error) {
	conn, err := amqp091.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	err = ch.ExchangeDeclare(
		exchange, // 交换机名称
		"direct", // 类型
		true,     // durable - 持久化
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare exchange: %w", err)
	}

	_, err = ch.QueueDeclare(
		queueName, // 队列名称
		true,      // durable - 持久化
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	err = ch.QueueBind(
		queueName,  // 队列名称
		routingKey, // 路由键
		exchange,   // 交换机
		false,      // no-wait
		nil,        // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind queue: %w", err)
	}

	// 设置 QoS（预取数量）
	err = ch.Qos(
		10,    // prefetch count - 每次预取10条消息
		0,     // prefetch size
		false, // global
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	logx.Infof("RabbitMQ consumer created: exchange=%s, routingKey=%s, queue=%s, consumerTag=%s",
		exchange, routingKey, queueName, consumerTag)

	return &Consumer{
		conn:        conn,
		channel:     ch,
		exchange:    exchange,
		routingKey:  routingKey,
		queueName:   queueName,
		processFunc: processFunc,
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
	}, nil
}

// Start 启动消费者
func (c *Consumer) Start() error {
	c.wg.Add(1)
	go c.consume()

	logx.Info("RabbitMQ consumer started")
	return nil
}

// consume 消费消息
func (c *Consumer) consume() {
	defer c.wg.Done()

	msgs, err := c.channel.Consume(
		c.queueName, // 队列名称
		"",          // consumer tag - 自动生成
		false,       // auto-ack - 手动确认
		false,       // exclusive
		false,       // no-local
		false,       // no-wait
		nil,         // arguments
	)
	if err != nil {
		logx.Errorf("Failed to register consumer: %v", err)
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			logx.Info("Consumer context cancelled, stopping...")
			return
		case msg, ok := <-msgs:
			if !ok {
				logx.Info("Message channel closed, reconnecting...")
				return
			}

			c.handleMessage(msg)
		}
	}
}

// handleMessage 处理单条消息
func (c *Consumer) handleMessage(msg amqp091.Delivery) {
	logx.Infof("Received message: routingKey=%s, body=%s", msg.RoutingKey, string(msg.Body))

	// 解析消息
	var seckillMsg SeckillOrderMessage
	if err := json.Unmarshal(msg.Body, &seckillMsg); err != nil {
		logx.Errorf("Failed to unmarshal message: %v, body=%s", err, string(msg.Body))
		msg.Reject(false)
		return
	}

	// 处理秒杀订单
	if c.processFunc != nil {
		if err := c.processFunc(&seckillMsg); err != nil {
			logx.Errorf("Failed to process seckill order: orderId=%s, err=%v", seckillMsg.OrderId, err)
			msg.Nack(false, true)
			return
		}
	}

	// 确认消息
	if err := msg.Ack(false); err != nil {
		logx.Errorf("Failed to ack message: %v", err)
	}

	logx.Infof("Successfully processed seckill order: orderId=%s", seckillMsg.OrderId)
}

// Stop 停止消费者
func (c *Consumer) Stop() error {
	c.cancel()
	c.wg.Wait()

	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}

	close(c.done)
	logx.Info("RabbitMQ consumer stopped")
	return nil
}
