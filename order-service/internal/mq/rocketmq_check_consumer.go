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

// TimeoutCheckFunc 超时检查处理函数类型
type TimeoutCheckFunc func(msg *SeckillOrderMessage) error

// RocketMQCheckConsumer 超时检查消费者（消费延迟5分钟后的检查消息）
type RocketMQCheckConsumer struct {
	c           rocketmq.PushConsumer
	processFunc TimeoutCheckFunc
}

// NewRocketMQCheckConsumer 创建超时检查消费者
func NewRocketMQCheckConsumer(cfg RocketMQConsumerConfig, processFunc TimeoutCheckFunc) (*RocketMQCheckConsumer, error) {
	group := cfg.CheckConsumerGroup
	if group == "" {
		group = "seckill_check_consumer"
	}
	topic := cfg.CheckTopic
	if topic == "" {
		topic = "seckill_order_check"
	}

	c, err := rocketmq.NewPushConsumer(
		consumer.WithNameServer([]string{cfg.NameServer}),
		consumer.WithGroupName(group),
		consumer.WithConsumeFromWhere(consumer.ConsumeFromLastOffset),
		consumer.WithConsumerModel(consumer.Clustering),
	)
	if err != nil {
		return nil, fmt.Errorf("create check consumer failed: %w", err)
	}

	cc := &RocketMQCheckConsumer{c: c, processFunc: processFunc}

	if err = c.Subscribe(topic, consumer.MessageSelector{},
		func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
			for _, msg := range msgs {
				if err := cc.handle(msg); err != nil {
					logx.Errorf("check consumer handle failed, will retry: msgId=%s, reconsumeTimes=%d, err=%v",
						msg.MsgId, msg.ReconsumeTimes, err)
					return consumer.ConsumeRetryLater, nil
				}
			}
			return consumer.ConsumeSuccess, nil
		}); err != nil {
		return nil, fmt.Errorf("subscribe check topic failed: %w", err)
	}

	logx.Infof("RocketMQ check consumer created: nameServer=%s, group=%s, topic=%s",
		cfg.NameServer, group, topic)
	return cc, nil
}

func (cc *RocketMQCheckConsumer) handle(msg *primitive.MessageExt) error {
	var seckillMsg SeckillOrderMessage
	if err := json.Unmarshal(msg.Body, &seckillMsg); err != nil {
		logx.Errorf("check consumer unmarshal failed, skip: msgId=%s, err=%v", msg.MsgId, err)
		return nil
	}
	return cc.processFunc(&seckillMsg)
}

// Start 启动消费者
func (cc *RocketMQCheckConsumer) Start() error {
	if err := cc.c.Start(); err != nil {
		return fmt.Errorf("start check consumer failed: %w", err)
	}
	logx.Info("RocketMQ check consumer started")
	return nil
}

// Stop 停止消费者
func (cc *RocketMQCheckConsumer) Stop() error {
	return cc.c.Shutdown()
}
