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
				{ID: 41, EventType: "order.created", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-41","event_type":"order.created","occurred_at":1710000300,"aggregate_type":"order","aggregate_id":"S108_S50004","trace_id":"trace-41","source":"order-service","version":1,"message_id":"S108_S50004","order_id":"S108_S50004","reservation_id":"S108_S50004","user_id":101,"seckill_product_id":108,"product_id":14,"quantity":1,"amount":15900,"shard_no":5,"status":2,"order_type":1,"pay_status":0}`},
				{ID: 42, EventType: "payment.succeeded", Status: OutboxStatusPublished, PayloadJSON: `{"payment_id":"P50004","order_id":"S108_S50004"}`},
				{ID: 43, EventType: "order.completed", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-43","event_type":"order.completed","occurred_at":1710000301,"aggregate_type":"order","aggregate_id":"S108_S50004","trace_id":"trace-43","source":"order-service","version":1,"message_id":"S108_S50004","order_id":"S108_S50004","reservation_id":"S108_S50004","payment_id":"P50004","user_id":101,"seckill_product_id":108,"product_id":14,"quantity":1,"amount":15900,"shard_no":5,"status":5,"order_type":1,"pay_status":2,"paid_at":1710000301}`},
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
				{ID: 61, EventType: "reservation.released", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-61","event_type":"reservation.released","occurred_at":1710000500,"aggregate_type":"reservation","aggregate_id":"S110_S50006","trace_id":"trace-61","source":"system","version":1,"message_id":"S110_S50006","reservation_id":"S110_S50006","order_id":"S110_S50006","user_id":103,"seckill_product_id":110,"product_id":16,"quantity":1,"amount":17900,"shard_no":7,"from_status":0,"status":6,"reason":"timeout_release"}`},
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

func TestRunnerFlagsManualAnomalyWhenPaymentRequestedEventIsMissing(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S111_S50007",
				ReservationID:                 "S111_S50007",
				PaymentID:                     "P50007",
				UserID:                        104,
				ProductID:                     17,
				Quantity:                      1,
				Amount:                        18900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710000600,
				SeckillProductID:              111,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             104,
				ReservationProductID:          17,
				ReservationQuantity:           1,
				ReservationAmount:             18900,
				ReservationStatus:             ReservationStatusConsumed,
				PaymentFound:                  true,
				PaymentUserID:                 104,
				PaymentAmount:                 18900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S111_S50007": {
				{ID: 71, EventType: "order.created", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-71","event_type":"order.created","occurred_at":1710000600,"aggregate_type":"order","aggregate_id":"S111_S50007","trace_id":"trace-71","source":"order-service","version":1,"message_id":"S111_S50007","reservation_id":"S111_S50007","order_id":"S111_S50007","user_id":104,"seckill_product_id":111,"product_id":17,"quantity":1,"amount":18900,"shard_no":3,"status":2,"order_type":1,"pay_status":0}`},
				{ID: 72, EventType: "payment.succeeded", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-72","event_type":"payment.succeeded","occurred_at":1710000601,"aggregate_type":"payment","aggregate_id":"P50007","trace_id":"trace-72","source":"order-service","version":1,"message_id":"S111_S50007","reservation_id":"S111_S50007","order_id":"S111_S50007","payment_id":"P50007","user_id":104,"seckill_product_id":111,"product_id":17,"quantity":1,"amount":18900,"shard_no":3,"status":2,"channel":"mock_alipay","third_party_trade_no":"trade-72","paid_at":1710000601,"callback_id":"cb-72"}`},
				{ID: 73, EventType: "order.completed", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-73","event_type":"order.completed","occurred_at":1710000601,"aggregate_type":"order","aggregate_id":"S111_S50007","trace_id":"trace-73","source":"order-service","version":1,"message_id":"S111_S50007","reservation_id":"S111_S50007","order_id":"S111_S50007","payment_id":"P50007","user_id":104,"seckill_product_id":111,"product_id":17,"quantity":1,"amount":18900,"shard_no":3,"status":5,"order_type":1,"pay_status":2,"paid_at":1710000601}`},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S111_S50007": "success"}}
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

	if got := sum.ManualAnomalyCount[AnomalyOutboxMissingPaymentRequested]; got != 1 {
		t.Fatalf("expected missing payment.requested anomaly count=1, got %d", got)
	}
}

func TestRunnerFlagsManualAnomalyWhenPaymentRequestedPayloadMissesUnifiedFields(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S112_S50008",
				ReservationID:                 "S112_S50008",
				PaymentID:                     "P50008",
				UserID:                        105,
				ProductID:                     18,
				Quantity:                      1,
				Amount:                        19900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710000700,
				SeckillProductID:              112,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             105,
				ReservationProductID:          18,
				ReservationQuantity:           1,
				ReservationAmount:             19900,
				ReservationStatus:             ReservationStatusConsumed,
				PaymentFound:                  true,
				PaymentUserID:                 105,
				PaymentAmount:                 19900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S112_S50008": {
				{ID: 81, EventType: "order.created", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-81","event_type":"order.created","occurred_at":1710000700,"aggregate_type":"order","aggregate_id":"S112_S50008","trace_id":"trace-81","source":"order-service","version":1,"message_id":"S112_S50008","reservation_id":"S112_S50008","order_id":"S112_S50008","user_id":105,"seckill_product_id":112,"product_id":18,"quantity":1,"amount":19900,"shard_no":4,"status":2,"order_type":1,"pay_status":0}`},
				{ID: 82, EventType: "payment.requested", Status: OutboxStatusPublished, PayloadJSON: `{"payment_id":"P50008","order_id":"S112_S50008"}`},
				{ID: 83, EventType: "payment.succeeded", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-83","event_type":"payment.succeeded","occurred_at":1710000701,"aggregate_type":"payment","aggregate_id":"P50008","trace_id":"trace-83","source":"order-service","version":1,"message_id":"S112_S50008","reservation_id":"S112_S50008","order_id":"S112_S50008","payment_id":"P50008","user_id":105,"seckill_product_id":112,"product_id":18,"quantity":1,"amount":19900,"shard_no":4,"status":2,"channel":"mock_alipay","third_party_trade_no":"trade-83","paid_at":1710000701,"callback_id":"cb-83"}`},
				{ID: 84, EventType: "order.completed", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-84","event_type":"order.completed","occurred_at":1710000701,"aggregate_type":"order","aggregate_id":"S112_S50008","trace_id":"trace-84","source":"order-service","version":1,"message_id":"S112_S50008","reservation_id":"S112_S50008","order_id":"S112_S50008","payment_id":"P50008","user_id":105,"seckill_product_id":112,"product_id":18,"quantity":1,"amount":19900,"shard_no":4,"status":5,"order_type":1,"pay_status":2,"paid_at":1710000701}`},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S112_S50008": "success"}}
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

func TestRunnerDoesNotFlagPaymentRequestedWhenPayloadIsComplete(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S113_S50009",
				ReservationID:                 "S113_S50009",
				PaymentID:                     "P50009",
				UserID:                        106,
				ProductID:                     19,
				Quantity:                      1,
				Amount:                        20900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710000800,
				SeckillProductID:              113,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             106,
				ReservationProductID:          19,
				ReservationQuantity:           1,
				ReservationAmount:             20900,
				ReservationStatus:             ReservationStatusConsumed,
				PaymentFound:                  true,
				PaymentUserID:                 106,
				PaymentAmount:                 20900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S113_S50009": {
				{ID: 91, EventType: "order.created", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-91","event_type":"order.created","occurred_at":1710000800,"aggregate_type":"order","aggregate_id":"S113_S50009","trace_id":"trace-91","source":"order-service","version":1,"message_id":"S113_S50009","reservation_id":"S113_S50009","order_id":"S113_S50009","user_id":106,"seckill_product_id":113,"product_id":19,"quantity":1,"amount":20900,"shard_no":6,"status":2,"order_type":1,"pay_status":0}`},
				{ID: 92, EventType: "payment.requested", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-92","event_type":"payment.requested","occurred_at":1710000800,"aggregate_type":"payment","aggregate_id":"P50009","trace_id":"trace-92","source":"order-service","version":1,"message_id":"S113_S50009","reservation_id":"S113_S50009","order_id":"S113_S50009","payment_id":"P50009","user_id":106,"seckill_product_id":113,"product_id":19,"quantity":1,"amount":20900,"shard_no":6,"status":1,"channel":"mock_alipay"}`},
				{ID: 93, EventType: "payment.succeeded", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-93","event_type":"payment.succeeded","occurred_at":1710000801,"aggregate_type":"payment","aggregate_id":"P50009","trace_id":"trace-93","source":"order-service","version":1,"message_id":"S113_S50009","reservation_id":"S113_S50009","order_id":"S113_S50009","payment_id":"P50009","user_id":106,"seckill_product_id":113,"product_id":19,"quantity":1,"amount":20900,"shard_no":6,"status":2,"channel":"mock_alipay","third_party_trade_no":"trade-93","paid_at":1710000801,"callback_id":"cb-93"}`},
				{ID: 94, EventType: "order.completed", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-94","event_type":"order.completed","occurred_at":1710000801,"aggregate_type":"order","aggregate_id":"S113_S50009","trace_id":"trace-94","source":"order-service","version":1,"message_id":"S113_S50009","reservation_id":"S113_S50009","order_id":"S113_S50009","payment_id":"P50009","user_id":106,"seckill_product_id":113,"product_id":19,"quantity":1,"amount":20900,"shard_no":6,"status":5,"order_type":1,"pay_status":2,"paid_at":1710000801}`},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S113_S50009": "success"}}
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

	if got := sum.ManualAnomalyCount[AnomalyOutboxMissingPaymentRequested]; got != 0 {
		t.Fatalf("expected no missing payment.requested anomaly, got %d", got)
	}
	if got := sum.ManualAnomalyCount[AnomalyOutboxPayloadMissingFields]; got != 0 {
		t.Fatalf("expected no payload missing fields anomaly, got %d", got)
	}
}

func TestRunnerFlagsManualAnomalyWhenOutboxEventOrderIsInvalid(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S114_S50010",
				ReservationID:                 "S114_S50010",
				PaymentID:                     "P50010",
				UserID:                        107,
				ProductID:                     20,
				Quantity:                      1,
				Amount:                        21900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710000900,
				SeckillProductID:              114,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             107,
				ReservationProductID:          20,
				ReservationQuantity:           1,
				ReservationAmount:             21900,
				ReservationStatus:             ReservationStatusConsumed,
				PaymentFound:                  true,
				PaymentUserID:                 107,
				PaymentAmount:                 21900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S114_S50010": {
				{ID: 101, EventType: "order.created", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-101","event_type":"order.created","occurred_at":1710000900,"aggregate_type":"order","aggregate_id":"S114_S50010","trace_id":"trace-101","source":"order-service","version":1,"message_id":"S114_S50010","reservation_id":"S114_S50010","order_id":"S114_S50010","user_id":107,"seckill_product_id":114,"product_id":20,"quantity":1,"amount":21900,"shard_no":8,"status":2,"order_type":1,"pay_status":0}`},
				{ID: 102, EventType: "payment.succeeded", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-102","event_type":"payment.succeeded","occurred_at":1710000901,"aggregate_type":"payment","aggregate_id":"P50010","trace_id":"trace-102","source":"order-service","version":1,"message_id":"S114_S50010","reservation_id":"S114_S50010","order_id":"S114_S50010","payment_id":"P50010","user_id":107,"seckill_product_id":114,"product_id":20,"quantity":1,"amount":21900,"shard_no":8,"status":2,"channel":"mock_alipay","third_party_trade_no":"trade-102","paid_at":1710000901,"callback_id":"cb-102"}`},
				{ID: 103, EventType: "payment.requested", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-103","event_type":"payment.requested","occurred_at":1710000902,"aggregate_type":"payment","aggregate_id":"P50010","trace_id":"trace-103","source":"order-service","version":1,"message_id":"S114_S50010","reservation_id":"S114_S50010","order_id":"S114_S50010","payment_id":"P50010","user_id":107,"seckill_product_id":114,"product_id":20,"quantity":1,"amount":21900,"shard_no":8,"status":1,"channel":"mock_alipay"}`},
				{ID: 104, EventType: "order.completed", Status: OutboxStatusPublished, PayloadJSON: `{"event_id":"evt-104","event_type":"order.completed","occurred_at":1710000902,"aggregate_type":"order","aggregate_id":"S114_S50010","trace_id":"trace-104","source":"order-service","version":1,"message_id":"S114_S50010","reservation_id":"S114_S50010","order_id":"S114_S50010","payment_id":"P50010","user_id":107,"seckill_product_id":114,"product_id":20,"quantity":1,"amount":21900,"shard_no":8,"status":5,"order_type":1,"pay_status":2,"paid_at":1710000902}`},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S114_S50010": "success"}}
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

	if got := sum.ManualAnomalyCount[AnomalyOutboxEventOrderInvalid]; got != 1 {
		t.Fatalf("expected outbox event order invalid anomaly count=1, got %d", got)
	}
}
