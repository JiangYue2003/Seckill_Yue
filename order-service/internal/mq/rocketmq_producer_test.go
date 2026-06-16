package mq

import "testing"

func TestDefaultEventTopic(t *testing.T) {
	cfg := RocketMQConfig{}
	applyRocketMQProducerDefaults(&cfg)
	if cfg.EventTopic != "order_event" {
		t.Fatalf("expected default event topic order_event, got %s", cfg.EventTopic)
	}
}
