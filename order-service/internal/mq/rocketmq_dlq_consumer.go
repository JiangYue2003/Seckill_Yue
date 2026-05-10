package mq

import (
	"context"
	"fmt"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/zeromicro/go-zero/core/logx"
)

// RocketMQDLQConsumer 死信队列监控消费者
// RocketMQ 超过最大重试次数（默认16次）后自动路由到 %DLQ%{ConsumerGroup}
// 此消费者负责监控死信消息，记录日志和告警，不做业务处理
type RocketMQDLQConsumer struct {
	c rocketmq.PushConsumer
}

// NewRocketMQDLQConsumer 创建死信队列监控消费者
func NewRocketMQDLQConsumer(cfg RocketMQConsumerConfig) (*RocketMQDLQConsumer, error) {
	dlqGroup := cfg.DLQConsumerGroup
	if dlqGroup == "" {
		dlqGroup = "seckill_dlq_consumer"
	}
	// RocketMQ 死信 Topic 格式：%DLQ%{原ConsumerGroup}
	orderDLQTopic := "%DLQ%" + cfg.OrderConsumerGroup
	if cfg.OrderConsumerGroup == "" {
		orderDLQTopic = "%DLQ%seckill_order_consumer"
	}

	c, err := rocketmq.NewPushConsumer(
		consumer.WithNameServer([]string{cfg.NameServer}),
		consumer.WithGroupName(dlqGroup),
		consumer.WithConsumeFromWhere(consumer.ConsumeFromLastOffset),
		consumer.WithConsumerModel(consumer.Clustering),
	)
	if err != nil {
		return nil, fmt.Errorf("create dlq consumer failed: %w", err)
	}

	if err = c.Subscribe(orderDLQTopic, consumer.MessageSelector{},
		func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
			for _, msg := range msgs {
				// 死信消息只记录告警日志，不做业务处理，不重试
				logx.Errorf("[DLQ ALERT] Dead letter message received: msgId=%s, topic=%s, keys=%v, reconsumeTimes=%d, body=%s",
					msg.MsgId, msg.Topic, msg.GetKeys(), msg.ReconsumeTimes, string(msg.Body))
			}
			return consumer.ConsumeSuccess, nil
		}); err != nil {
		return nil, fmt.Errorf("subscribe dlq topic failed: %w", err)
	}

	logx.Infof("RocketMQ DLQ consumer created: nameServer=%s, group=%s, dlqTopic=%s",
		cfg.NameServer, dlqGroup, orderDLQTopic)
	return &RocketMQDLQConsumer{c: c}, nil
}

// Start 启动消费者
func (d *RocketMQDLQConsumer) Start() error {
	if err := d.c.Start(); err != nil {
		return fmt.Errorf("start dlq consumer failed: %w", err)
	}
	logx.Info("RocketMQ DLQ consumer started")
	return nil
}

// Stop 停止消费者
func (d *RocketMQDLQConsumer) Stop() error {
	return d.c.Shutdown()
}
