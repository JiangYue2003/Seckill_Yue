package mq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/zeromicro/go-zero/core/logx"
)

// DelayLevel5Min RocketMQ 延迟级别 9 对应 5 分钟
// 延迟级别对应时间：1s 5s 10s 30s 1m 2m 3m 4m 5m 6m 7m 8m 9m 10m 20m 30m 1h 2h
const DelayLevel5Min = 9

// RocketMQProducer RocketMQ 同步生产者
type RocketMQProducer struct {
	p          rocketmq.Producer
	orderTopic string
	checkTopic string
}

// RocketMQConfig 生产者配置
type RocketMQConfig struct {
	NameServer    string
	ProducerGroup string
	OrderTopic    string
	CheckTopic    string
}

// NewRocketMQProducer 创建 RocketMQ 生产者
func NewRocketMQProducer(cfg RocketMQConfig) (*RocketMQProducer, error) {
	if cfg.ProducerGroup == "" {
		cfg.ProducerGroup = "seckill_producer"
	}
	if cfg.OrderTopic == "" {
		cfg.OrderTopic = "seckill_order"
	}
	if cfg.CheckTopic == "" {
		cfg.CheckTopic = "seckill_order_check"
	}

	p, err := rocketmq.NewProducer(
		producer.WithNameServer([]string{cfg.NameServer}),
		producer.WithGroupName(cfg.ProducerGroup),
		producer.WithRetry(2),
	)
	if err != nil {
		return nil, fmt.Errorf("create rocketmq producer failed: %w", err)
	}
	if err = p.Start(); err != nil {
		return nil, fmt.Errorf("start rocketmq producer failed: %w", err)
	}

	logx.Infof("RocketMQ producer started: nameServer=%s, group=%s, orderTopic=%s, checkTopic=%s",
		cfg.NameServer, cfg.ProducerGroup, cfg.OrderTopic, cfg.CheckTopic)

	return &RocketMQProducer{
		p:          p,
		orderTopic: cfg.OrderTopic,
		checkTopic: cfg.CheckTopic,
	}, nil
}

// SendSeckillOrder 发送主链路消息（立即投递）
func (r *RocketMQProducer) SendSeckillOrder(ctx context.Context, msg *SeckillOrderMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal seckill message failed: %w", err)
	}
	m := primitive.NewMessage(r.orderTopic, body)
	m.WithKeys([]string{msg.OrderId})

	result, err := r.p.SendSync(ctx, m)
	if err != nil {
		logx.Errorf("RocketMQ send order failed: orderId=%s, err=%v", msg.OrderId, err)
		return fmt.Errorf("send seckill order failed: %w", err)
	}
	logx.Infof("RocketMQ send order success: orderId=%s, msgId=%s", msg.OrderId, result.MsgID)
	return nil
}

// SendDelayOrder 发送延迟检查消息（5分钟后投递，DelayLevel=9）
func (r *RocketMQProducer) SendDelayOrder(ctx context.Context, msg *SeckillOrderMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal delay message failed: %w", err)
	}
	m := primitive.NewMessage(r.checkTopic, body)
	m.WithDelayTimeLevel(DelayLevel5Min)
	m.WithKeys([]string{msg.OrderId})

	result, err := r.p.SendSync(ctx, m)
	if err != nil {
		logx.Errorf("RocketMQ send delay failed: orderId=%s, err=%v", msg.OrderId, err)
		return fmt.Errorf("send delay order failed: %w", err)
	}
	logx.Infof("RocketMQ send delay success: orderId=%s, msgId=%s, delayLevel=%d",
		msg.OrderId, result.MsgID, DelayLevel5Min)
	return nil
}

// Close 关闭生产者
func (r *RocketMQProducer) Close() error {
	return r.p.Shutdown()
}
