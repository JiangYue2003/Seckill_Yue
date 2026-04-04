package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/zeromicro/go-zero/core/logx"
)

// 队列/交换机名称
const (
	SeckillOrderQueueName = "seckill_order_queue"       // 主处理队列
	SeckillDLXName        = "seckill_dlx"               // 死信交换机（DLX）
	SeckillDeadQueueName  = "seckill_dead_queue"        // 死信队列
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
	MaxRetryCount      = 3                // 最大重试次数
	OrderCheckDelayMs  = 300000           // 延迟队列TTL: 5分钟（毫秒）
	consumerReconnBase = time.Second      // 消费者重连基础等待
	consumerReconnMax  = 30 * time.Second // 消费者重连最大等待
)

// SeckillOrderMessage 秒杀成功消息
type SeckillOrderMessage struct {
	OrderId          string `json:"order_id"`
	UserId           int64  `json:"user_id"`
	SeckillProductId int64  `json:"seckill_product_id"`
	ProductId        int64  `json:"product_id"`
	Quantity         int64  `json:"quantity"`
	SeckillPrice     int64  `json:"seckill_price"`
	Amount           int64  `json:"amount"`
	CreatedAt        int64  `json:"created_at"`
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
	consumerTag string
	url         string // 保存连接串，重连复用
	processFunc ProcessFunc
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	done        chan struct{}
}

// NewConsumer 创建 RabbitMQ 消费者
func NewConsumer(url, exchange, routingKey, queueName, consumerTag string, processFunc ProcessFunc) (*Consumer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Consumer{
		url:         url,
		exchange:    exchange,
		routingKey:  routingKey,
		queueName:   queueName,
		consumerTag: consumerTag,
		processFunc: processFunc,
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
	}

	if err := c.setupConnection(); err != nil {
		cancel()
		return nil, err
	}

	logx.Infof("RabbitMQ consumer created: exchange=%s, routingKey=%s, queue=%s",
		exchange, routingKey, queueName)
	return c, nil
}

// setupConnection 建立连接、channel，并声明完整拓扑
func (c *Consumer) setupConnection() error {
	conn, err := amqp091.Dial(c.url)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel: %w", err)
	}

	if err = c.setupTopology(ch, c.exchange); err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	if err = ch.Qos(10, 0, false); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	c.conn = conn
	c.channel = ch
	return nil
}

// setupTopology 声明所有队列、交换机和绑定关系（供初始化和重连复用）
func (c *Consumer) setupTopology(ch *amqp091.Channel, exchange string) error {
	// 主交换机
	if err := ch.ExchangeDeclare(exchange, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare exchange: %w", err)
	}
	// DLX 交换机
	if err := ch.ExchangeDeclare(SeckillDLXName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare DLX exchange: %w", err)
	}
	// 死信队列
	if _, err := ch.QueueDeclare(SeckillDeadQueueName, true, false, false, false, nil); err != nil {
		return fmt.Errorf("failed to declare dead queue: %w", err)
	}
	if err := ch.QueueBind(SeckillDeadQueueName, RoutingKeyDead, SeckillDLXName, false, nil); err != nil {
		return fmt.Errorf("failed to bind dead queue: %w", err)
	}
	// 主处理队列（含 DLX）
	mainArgs := amqp091.Table{
		"x-dead-letter-exchange":    SeckillDLXName,
		"x-dead-letter-routing-key": RoutingKeyDead,
	}
	if _, err := ch.QueueDeclare(c.queueName, true, false, false, false, mainArgs); err != nil {
		return fmt.Errorf("failed to declare main queue: %w", err)
	}
	if err := ch.QueueBind(c.queueName, c.routingKey, exchange, false, nil); err != nil {
		return fmt.Errorf("failed to bind main queue: %w", err)
	}
	// 延迟队列（无消费者）
	delayArgs := amqp091.Table{
		"x-message-ttl":             int32(OrderCheckDelayMs),
		"x-dead-letter-exchange":    exchange,
		"x-dead-letter-routing-key": RoutingKeyCheck,
	}
	if _, err := ch.QueueDeclare(SeckillDelayQueueName, true, false, false, false, delayArgs); err != nil {
		return fmt.Errorf("failed to declare delay queue: %w", err)
	}
	if err := ch.QueueBind(SeckillDelayQueueName, RoutingKeyDelay, exchange, false, nil); err != nil {
		return fmt.Errorf("failed to bind delay queue: %w", err)
	}
	// 超时检查队列（含 DLX）
	checkArgs := amqp091.Table{
		"x-dead-letter-exchange":    SeckillDLXName,
		"x-dead-letter-routing-key": RoutingKeyDead,
	}
	if _, err := ch.QueueDeclare(SeckillCheckQueueName, true, false, false, false, checkArgs); err != nil {
		return fmt.Errorf("failed to declare check queue: %w", err)
	}
	if err := ch.QueueBind(SeckillCheckQueueName, RoutingKeyCheck, exchange, false, nil); err != nil {
		return fmt.Errorf("failed to bind check queue: %w", err)
	}
	return nil
}

// reconnect 断线后重建连接，指数退避直到成功（在 consume 内循环调用）
func (c *Consumer) reconnect(backoff *time.Duration) {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		time.Sleep(*backoff)
		if err := c.setupConnection(); err != nil {
			logx.Errorf("Consumer 重连失败: %v，%v 后重试", err, *backoff)
			if *backoff < consumerReconnMax {
				*backoff *= 2
			}
			continue
		}
		logx.Infof("Consumer 重连成功: queue=%s", c.queueName)
		*backoff = consumerReconnBase // 重置退避
		return
	}
}

// Start 启动消费者
func (c *Consumer) Start() error {
	c.wg.Add(1)
	go c.consume()
	logx.Infof("RabbitMQ consumer started: queue=%s", c.queueName)
	return nil
}

// consume 消费消息主循环，连接断开时自动重连
func (c *Consumer) consume() {
	defer c.wg.Done()
	backoff := consumerReconnBase

	for {
		// 检查是否已请求关闭
		select {
		case <-c.ctx.Done():
			logx.Infof("Consumer stopping: queue=%s", c.queueName)
			return
		default:
		}

		msgs, err := c.channel.Consume(c.queueName, "", false, false, false, false, nil)
		if err != nil {
			logx.Errorf("Consumer 注册失败（channel 可能已断开）: %v", err)
			c.reconnect(&backoff)
			continue
		}

		backoff = consumerReconnBase // 注册成功，退避重置
		logx.Infof("Consumer 开始消费: queue=%s", c.queueName)

	innerLoop:
		for {
			select {
			case <-c.ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					logx.Errorf("Consumer channel 关闭，准备重连: queue=%s", c.queueName)
					if backoff < consumerReconnMax {
						backoff *= 2
					}
					c.reconnect(&backoff)
					break innerLoop
				}
				c.handleMessage(msg)
			}
		}
	}
}

// handleMessage 处理单条消息
func (c *Consumer) handleMessage(msg amqp091.Delivery) {
	var seckillMsg SeckillOrderMessage
	if err := json.Unmarshal(msg.Body, &seckillMsg); err != nil {
		logx.Errorf("Failed to unmarshal message: %v", err)
		msg.Reject(false)
		return
	}

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

	if c.processFunc != nil {
		if err := c.processFunc(&seckillMsg); err != nil {
			logx.Errorf("Failed to process: orderId=%s, retryCount=%d, err=%v",
				seckillMsg.OrderId, retryCount, err)
			if retryCount >= MaxRetryCount {
				logx.Errorf("Max retry exceeded, routing to DLX: orderId=%s", seckillMsg.OrderId)
				msg.Nack(false, false) // DLX 自动路由
			} else {
				msg.Nack(false, true) // 重新入队重试
			}
			return
		}
	}

	if err := msg.Ack(false); err != nil {
		logx.Errorf("Failed to ack: %v", err)
	}
	logx.Infof("Processed: orderId=%s", seckillMsg.OrderId)
}

// Stop 优雅停止消费者
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
	logx.Infof("RabbitMQ consumer stopped: queue=%s", c.queueName)
	return nil
}
