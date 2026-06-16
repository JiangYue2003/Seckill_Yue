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
