package mq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"seckill-mall/order-service/internal/model/entity"

	"github.com/rabbitmq/amqp091-go"
	"github.com/zeromicro/go-zero/core/logx"
)

type RabbitMQProducerConfig struct {
	URL      string
	Exchange string
}

type Producer struct {
	conn     *amqp091.Connection
	channel  *amqp091.Channel
	url      string
	exchange string
	mu       sync.Mutex
}

func NewProducer(cfg RabbitMQProducerConfig) (*Producer, error) {
	applyRabbitMQProducerDefaults(&cfg)

	p := &Producer{
		url:      cfg.URL,
		exchange: cfg.Exchange,
	}
	if err := p.connect(); err != nil {
		return nil, err
	}
	p.monitorConnection()
	logx.Infof("RabbitMQ event producer created: exchange=%s", p.exchange)
	return p, nil
}

func applyRabbitMQProducerDefaults(cfg *RabbitMQProducerConfig) {
	if cfg == nil {
		return
	}
	if cfg.URL == "" {
		cfg.URL = defaultRabbitMQURL
	}
	if cfg.Exchange == "" {
		cfg.Exchange = defaultRabbitMQExchange
	}
}

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

	if err := ch.ExchangeDeclare(p.exchange, "direct", true, false, false, false, nil); err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	p.conn = conn
	p.channel = ch
	return nil
}

func (p *Producer) reconnect() error {
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	if err := p.connect(); err != nil {
		return err
	}
	p.monitorConnection()
	logx.Infof("RabbitMQ event producer reconnected")
	return nil
}

func (p *Producer) monitorConnection() {
	notify := p.conn.NotifyClose(make(chan *amqp091.Error, 1))
	go func() {
		amqpErr := <-notify
		if amqpErr == nil {
			return
		}
		logx.Errorf("RabbitMQ event producer connection closed: %v", amqpErr)
		backoff := consumerReconnBase
		for attempt := 1; ; attempt++ {
			time.Sleep(backoff)
			p.mu.Lock()
			err := p.reconnect()
			p.mu.Unlock()
			if err == nil {
				logx.Infof("RabbitMQ event producer reconnect success: attempt=%d", attempt)
				return
			}
			logx.Errorf("RabbitMQ event producer reconnect failed: attempt=%d err=%v", attempt, err)
			if backoff < consumerReconnMax {
				backoff *= 2
			}
		}
	}()
}

func (p *Producer) PublishEvent(ctx context.Context, event entity.EventOutbox) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	routingKey, body, err := buildEventPublishMessage(event)
	if err != nil {
		return err
	}
	if err := p.channel.PublishWithContext(ctx, p.exchange, routingKey, false, false, amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
	}); err != nil {
		return fmt.Errorf("publish order outbox event failed: %w", err)
	}
	logx.Infof("RabbitMQ publish order event success: eventId=%s, eventType=%s, routingKey=%s",
		event.EventId, event.EventType, routingKey)
	return nil
}

func buildEventPublishMessage(event entity.EventOutbox) (string, []byte, error) {
	if event.EventType == "" {
		return "", nil, fmt.Errorf("event type is empty")
	}
	return "event." + event.EventType, []byte(event.PayloadJSON), nil
}

func (p *Producer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	return nil
}
