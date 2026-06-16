package mq

import (
	"testing"

	"seckill-mall/order-service/internal/model/entity"
)

func TestApplyRabbitMQProducerDefaults(t *testing.T) {
	cfg := RabbitMQProducerConfig{}
	applyRabbitMQProducerDefaults(&cfg)
	if cfg.URL != defaultRabbitMQURL {
		t.Fatalf("expected default URL %s, got %s", defaultRabbitMQURL, cfg.URL)
	}
	if cfg.Exchange != defaultRabbitMQExchange {
		t.Fatalf("expected default exchange %s, got %s", defaultRabbitMQExchange, cfg.Exchange)
	}
}

func TestBuildEventPublishMessageUsesEventRoutingKey(t *testing.T) {
	event := entity.EventOutbox{
		EventId:       "evt-21",
		AggregateId:   "order-21",
		EventType:     "payment.succeeded",
		PayloadJSON:   `{"payment_id":"pay-21","order_id":"order-21"}`,
		AggregateType: "payment",
	}

	routingKey, body, err := buildEventPublishMessage(event)
	if err != nil {
		t.Fatalf("buildEventPublishMessage() error = %v", err)
	}
	if routingKey != "event.payment.succeeded" {
		t.Fatalf("expected routing key event.payment.succeeded, got %s", routingKey)
	}
	if string(body) != event.PayloadJSON {
		t.Fatalf("expected payload passthrough, got %s", string(body))
	}
}
