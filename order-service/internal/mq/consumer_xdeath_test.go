package mq

import (
	"testing"

	"github.com/rabbitmq/amqp091-go"
)

func TestGetRetryCountFromXDeath_NoHeaders(t *testing.T) {
	got := getRetryCountFromXDeath(nil, SeckillCheckQueueName)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestGetRetryCountFromXDeath_NoXDeath(t *testing.T) {
	headers := amqp091.Table{
		"foo": "bar",
	}
	got := getRetryCountFromXDeath(headers, SeckillCheckQueueName)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestGetRetryCountFromXDeath_MatchQueueInt64(t *testing.T) {
	headers := amqp091.Table{
		"x-death": []interface{}{
			amqp091.Table{
				"queue": SeckillCheckQueueName,
				"count": int64(2),
			},
		},
	}

	got := getRetryCountFromXDeath(headers, SeckillCheckQueueName)
	if got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}

func TestGetRetryCountFromXDeath_OnlyCurrentQueue(t *testing.T) {
	headers := amqp091.Table{
		"x-death": []interface{}{
			amqp091.Table{
				"queue": "other_queue",
				"count": int64(9),
			},
			amqp091.Table{
				"queue": SeckillCheckQueueName,
				"count": int64(3),
			},
		},
	}

	got := getRetryCountFromXDeath(headers, SeckillCheckQueueName)
	if got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestGetRetryCountFromXDeath_MapEntryFloatCount(t *testing.T) {
	headers := amqp091.Table{
		"x-death": []interface{}{
			map[string]interface{}{
				"queue": SeckillCheckQueueName,
				"count": float64(4),
			},
		},
	}

	got := getRetryCountFromXDeath(headers, SeckillCheckQueueName)
	if got != 4 {
		t.Fatalf("expected 4, got %d", got)
	}
}

func TestGetRetryCountFromXDeath_InvalidType(t *testing.T) {
	headers := amqp091.Table{
		"x-death": "invalid",
	}

	got := getRetryCountFromXDeath(headers, SeckillCheckQueueName)
	if got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}
