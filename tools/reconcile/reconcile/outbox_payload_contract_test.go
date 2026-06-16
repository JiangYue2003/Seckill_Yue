package reconcile

import (
	"context"
	"testing"
)

func TestRunnerFlagsManualAnomalyWhenPaymentSucceededPayloadMissesUnifiedFields(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S108_S50004",
				ReservationID:                 "S108_S50004",
				PaymentID:                     "P50004",
				UserID:                        101,
				ProductID:                     14,
				Quantity:                      1,
				Amount:                        15900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710000300,
				SeckillProductID:              108,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             101,
				ReservationProductID:          14,
				ReservationQuantity:           1,
				ReservationAmount:             15900,
				ReservationStatus:             ReservationStatusConsumed,
				PaymentFound:                  true,
				PaymentUserID:                 101,
				PaymentAmount:                 15900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S108_S50004": {
				{ID: 41, EventType: "order.created", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-41","event_type":"order.created","occurred_at":1710000300,"aggregate_type":"order","aggregate_id":"S108_S50004","trace_id":"trace-41","source":"order-service","version":1,"message_id":"S108_S50004","order_id":"S108_S50004","reservation_id":"S108_S50004","user_id":101,"seckill_product_id":108,"product_id":14,"quantity":1,"amount":15900,"status":2,"order_type":1,"pay_status":0}`},
				{ID: 42, EventType: "payment.succeeded", Status: OutboxStatusPublished, PayloadJSON: `{"payment_id":"P50004","order_id":"S108_S50004"}`},
				{ID: 43, EventType: "order.completed", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-43","event_type":"order.completed","occurred_at":1710000301,"aggregate_type":"order","aggregate_id":"S108_S50004","trace_id":"trace-43","source":"order-service","version":1,"message_id":"S108_S50004","order_id":"S108_S50004","reservation_id":"S108_S50004","payment_id":"P50004","user_id":101,"seckill_product_id":108,"product_id":14,"quantity":1,"amount":15900,"status":5,"order_type":1,"pay_status":2,"paid_at":1710000301}`},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S108_S50004": "success"}}
	client := &fakeSeckillClient{}

	runner, err := NewRunner(Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       10,
		DryRun:          false,
		MaxRepair:       10,
	}, repo, store, client, nil)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := sum.ManualAnomalyCount[AnomalyOutboxPayloadMissingFields]; got != 1 {
		t.Fatalf("expected outbox payload missing fields anomaly count=1, got %d", got)
	}
}

func TestRunnerFlagsManualAnomalyWhenReservationReleasedPayloadMissesUnifiedFields(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                 "S109_S50005",
				ReservationID:           "S109_S50005",
				UserID:                  102,
				ProductID:               15,
				Quantity:                1,
				Amount:                  16900,
				Status:                  OrderStatusFailed,
				PayStatus:               OrderPayStatusInit,
				CreatedAt:               1710000400,
				SeckillProductID:        109,
				SeckillQuantity:         1,
				ReservationFound:        true,
				ReservationUserID:       102,
				ReservationProductID:    15,
				ReservationQuantity:     1,
				ReservationAmount:       16900,
				ReservationStatus:       ReservationStatusReleased,
				ProcessedMessageFound:   true,
				ProcessedMessageStatus:  ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S109_S50005": {
				{ID: 51, EventType: "reservation.released", Status: OutboxStatusPublished, PayloadJSON: `{"reservation_id":"S109_S50005","order_id":"S109_S50005","status":6}`},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S109_S50005": "failed"}}
	client := &fakeSeckillClient{}

	runner, err := NewRunner(Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       10,
		DryRun:          false,
		MaxRepair:       10,
	}, repo, store, client, nil)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := sum.ManualAnomalyCount[AnomalyOutboxPayloadMissingFields]; got != 1 {
		t.Fatalf("expected outbox payload missing fields anomaly count=1, got %d", got)
	}
}

func TestRunnerDoesNotFlagReservationReleasedWhenPayloadIsComplete(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                "S110_S50006",
				ReservationID:          "S110_S50006",
				UserID:                 103,
				ProductID:              16,
				Quantity:               1,
				Amount:                 17900,
				Status:                 OrderStatusFailed,
				PayStatus:              OrderPayStatusInit,
				CreatedAt:              1710000500,
				SeckillProductID:       110,
				SeckillQuantity:        1,
				ReservationFound:       true,
				ReservationUserID:      103,
				ReservationProductID:   16,
				ReservationQuantity:    1,
				ReservationAmount:      17900,
				ReservationStatus:      ReservationStatusReleased,
				ProcessedMessageFound:  true,
				ProcessedMessageStatus: ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S110_S50006": {
				{ID: 61, EventType: "reservation.released", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-61","event_type":"reservation.released","occurred_at":1710000500,"aggregate_type":"reservation","aggregate_id":"S110_S50006","trace_id":"trace-61","source":"system","version":1,"message_id":"S110_S50006","reservation_id":"S110_S50006","order_id":"S110_S50006","user_id":103,"seckill_product_id":110,"product_id":16,"quantity":1,"amount":17900,"from_status":0,"status":6,"reason":"timeout_release"}`},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S110_S50006": "failed"}}
	client := &fakeSeckillClient{}

	runner, err := NewRunner(Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       10,
		DryRun:          false,
		MaxRepair:       10,
	}, repo, store, client, nil)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := sum.ManualAnomalyCount[AnomalyOutboxPayloadMissingFields]; got != 0 {
		t.Fatalf("expected no outbox payload missing fields anomaly, got %d", got)
	}
}
