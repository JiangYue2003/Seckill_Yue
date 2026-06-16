package mq

import (
	"context"
	"fmt"

	"seckill-mall/order-service/internal/model/entity"

	"github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/zeromicro/go-zero/core/logx"
)

type rocketMQSyncProducer interface {
	Start() error
	SendSync(ctx context.Context, msgs ...*primitive.Message) (*primitive.SendResult, error)
	Shutdown() error
}

type RocketMQProducer struct {
	p          rocketMQSyncProducer
	eventTopic string
}

type RocketMQConfig struct {
	NameServer    string
	ProducerGroup string
	EventTopic    string
}

func NewRocketMQProducer(cfg RocketMQConfig) (*RocketMQProducer, error) {
	applyRocketMQProducerDefaults(&cfg)

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

	logx.Infof("RocketMQ producer started: nameServer=%s, group=%s, eventTopic=%s",
		cfg.NameServer, cfg.ProducerGroup, cfg.EventTopic)

	return &RocketMQProducer{
		p:          p,
		eventTopic: cfg.EventTopic,
	}, nil
}

func applyRocketMQProducerDefaults(cfg *RocketMQConfig) {
	if cfg == nil {
		return
	}
	if cfg.ProducerGroup == "" {
		cfg.ProducerGroup = "order_event_producer"
	}
	if cfg.EventTopic == "" {
		cfg.EventTopic = "order_event"
	}
}

func (r *RocketMQProducer) PublishEvent(ctx context.Context, event entity.EventOutbox) error {
	if r == nil || r.p == nil {
		return fmt.Errorf("rocketmq producer is nil")
	}
	message := primitive.NewMessage(r.eventTopic, []byte(event.PayloadJSON))
	message.WithKeys([]string{event.AggregateId, event.EventId})
	result, err := r.p.SendSync(ctx, message)
	if err != nil {
		return fmt.Errorf("publish order outbox event failed: %w", err)
	}
	logx.Infof("RocketMQ publish order event success: eventId=%s, eventType=%s, msgId=%s", event.EventId, event.EventType, result.MsgID)
	return nil
}

func (r *RocketMQProducer) Close() error {
	if r == nil || r.p == nil {
		return nil
	}
	return r.p.Shutdown()
}
