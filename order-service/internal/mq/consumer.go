package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/rabbitmq/amqp091-go"
	"github.com/zeromicro/go-zero/core/logx"
)

// 队列/交换机名称
const (
	SeckillOrderQueueName = "seckill_order_queue"       // 主处理队列
	SeckillDLXName        = "seckill_dlx"               // 死信交换机（DLX）
	SeckillDeadQueueName  = "seckill_dead_queue"        // 死信队列（替代原 seckill_order_dlq）
	SeckillDelayQueueName = "seckill_delay_queue"       // 延迟队列（无消费者，5min TTL）
	SeckillCheckQueueName = "seckill_order_check_queue" // 超时检查队列
)

// 路由键
const (
	RoutingKeyDead  = "seckill.dead"        // 死信路由键
	RoutingKeyDelay = "seckill.delay"       // 延迟消息路由键
	RoutingKeyCheck = "seckill.order.check" // 超时检查路由键
)

const (
	MaxRetryCount     = 3      // 最大重试次数
	OrderCheckDelayMs = 300000 // 延迟队列TTL: 5分钟（毫秒）
)

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

	// ── 声明死信交换机（DLX）──────────────────────────────────────
	if err = ch.ExchangeDeclare(SeckillDLXName, "direct", true, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare DLX exchange: %w", err)
	}

	// ── 声明死信队列并绑定到 DLX ──────────────────────────────────
	if _, err = ch.QueueDeclare(SeckillDeadQueueName, true, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare dead queue: %w", err)
	}
	if err = ch.QueueBind(SeckillDeadQueueName, RoutingKeyDead, SeckillDLXName, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind dead queue: %w", err)
	}

	// ── 声明主处理队列（含 DLX 参数）─────────────────────────────
	// 注意：若旧队列已存在且无 DLX 参数，需先在 RabbitMQ 控制台手动删除后重启服务
	mainQueueArgs := amqp091.Table{
		"x-dead-letter-exchange":    SeckillDLXName,
		"x-dead-letter-routing-key": RoutingKeyDead,
	}
	if _, err = ch.QueueDeclare(queueName, true, false, false, false, mainQueueArgs); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare main queue (if args conflict, delete old queue first): %w", err)
	}
	if err = ch.QueueBind(queueName, routingKey, exchange, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind main queue: %w", err)
	}

	// ── 声明延迟队列（无消费者，TTL 到期后路由到 check 队列）──────
	delayQueueArgs := amqp091.Table{
		"x-message-ttl":             int32(OrderCheckDelayMs),
		"x-dead-letter-exchange":    exchange,        // 过期后回到主交换机
		"x-dead-letter-routing-key": RoutingKeyCheck, // 路由到超时检查队列
	}
	if _, err = ch.QueueDeclare(SeckillDelayQueueName, true, false, false, false, delayQueueArgs); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare delay queue: %w", err)
	}
	if err = ch.QueueBind(SeckillDelayQueueName, RoutingKeyDelay, exchange, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind delay queue: %w", err)
	}

	// ── 声明超时检查队列（含 DLX 参数，处理失败也走死信）──────────
	checkQueueArgs := amqp091.Table{
		"x-dead-letter-exchange":    SeckillDLXName,
		"x-dead-letter-routing-key": RoutingKeyDead,
	}
	if _, err = ch.QueueDeclare(SeckillCheckQueueName, true, false, false, false, checkQueueArgs); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare check queue: %w", err)
	}
	if err = ch.QueueBind(SeckillCheckQueueName, RoutingKeyCheck, exchange, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to bind check queue: %w", err)
	}

	// 设置 QoS（预取数量）
	err = ch.Qos(
		10,    // prefetch count
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
		msg.Reject(false) // 格式错误直接丢弃，不重试
		return
	}

	// 获取重试次数
	retryCount := 0
	if msg.Headers != nil {
		if rc, ok := msg.Headers["x-retry-count"].(int64); ok {
			retryCount = int(rc)
		} else if rc, ok := msg.Headers["x-retry-count"].(int32); ok {
			retryCount = int(rc)
		} else if rc, ok := msg.Headers["x-retry-count"].(string); ok {
			retryCount, _ = strconv.Atoi(rc)
		}
	}

	// 处理秒杀订单
	if c.processFunc != nil {
		if err := c.processFunc(&seckillMsg); err != nil {
			logx.Errorf("Failed to process seckill order: orderId=%s, retryCount=%d, err=%v",
				seckillMsg.OrderId, retryCount, err)

			if retryCount >= MaxRetryCount {
				// 超过最大重试次数，Nack(requeue=false) 触发 DLX 自动路由到 seckill_dead_queue
				logx.Errorf("Max retry exceeded, routing to dead queue via DLX: orderId=%s", seckillMsg.OrderId)
				msg.Nack(false, false)
			} else {
				// 重试次数未达上限，Nack 并 requeue
				msg.Nack(false, true)
			}
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
