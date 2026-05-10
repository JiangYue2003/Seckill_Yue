package mq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/zeromicro/go-zero/core/logx"
)

// RocketMQConsumerConfig 消费者配置
type RocketMQConsumerConfig struct {
	NameServer         string
	OrderConsumerGroup string
	CheckConsumerGroup string
	DLQConsumerGroup   string
	OrderTopic         string
	CheckTopic         string
}

// RocketMQOrderConsumer 主链路消费者（订单落库）
type RocketMQOrderConsumer struct {
	c           rocketmq.PushConsumer
	processFunc ProcessFunc
}

// NewRocketMQOrderConsumer 创建主链路消费者
func NewRocketMQOrderConsumer(cfg RocketMQConsumerConfig, processFunc ProcessFunc) (*RocketMQOrderConsumer, error) {
	group := cfg.OrderConsumerGroup
	if group == "" {
		group = "seckill_order_consumer"
	}
	topic := cfg.OrderTopic
	if topic == "" {
		topic = "seckill_order"
	}

	c, err := rocketmq.NewPushConsumer(
		consumer.WithNameServer([]string{cfg.NameServer}),
		consumer.WithGroupName(group),
		consumer.WithConsumeFromWhere(consumer.ConsumeFromLastOffset),
		consumer.WithConsumerModel(consumer.Clustering),
	)
	if err != nil {
		return nil, fmt.Errorf("create order consumer failed: %w", err)
	}

	oc := &RocketMQOrderConsumer{c: c, processFunc: processFunc}

	if err = c.Subscribe(topic, consumer.MessageSelector{},
		func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
			for _, msg := range msgs {
				if err := oc.handle(msg); err != nil {
					// 可重试错误：返回 ConsumeRetryLater，RocketMQ 自动按延迟级别重试（最多16次）
					// 超过16次后自动进入 %DLQ%{ConsumerGroup}
					logx.Errorf("order consumer handle failed, will retry: msgId=%s, reconsumeTimes=%d, err=%v",
						msg.MsgId, msg.ReconsumeTimes, err)
					return consumer.ConsumeRetryLater, nil
				}
			}
			return consumer.ConsumeSuccess, nil
		}); err != nil {
		return nil, fmt.Errorf("subscribe order topic failed: %w", err)
	}

	logx.Infof("RocketMQ order consumer created: nameServer=%s, group=%s, topic=%s",
		cfg.NameServer, group, topic)
	return oc, nil
}

func (oc *RocketMQOrderConsumer) handle(msg *primitive.MessageExt) error {
	var seckillMsg SeckillOrderMessage
	if err := json.Unmarshal(msg.Body, &seckillMsg); err != nil {
		// 反序列化失败是不可重试的错误，记录日志后跳过（返回 nil 让 RocketMQ 认为成功）
		logx.Errorf("order consumer unmarshal failed, skip: msgId=%s, err=%v", msg.MsgId, err)
		return nil
	}
	return oc.processFunc(&seckillMsg)
}

// Start 启动消费者
func (oc *RocketMQOrderConsumer) Start() error {
	if err := oc.c.Start(); err != nil {
		return fmt.Errorf("start order consumer failed: %w", err)
	}
	logx.Info("RocketMQ order consumer started")
	return nil
}

// Stop 停止消费者
func (oc *RocketMQOrderConsumer) Stop() error {
	return oc.c.Shutdown()
}
