package reconcile

import (
	"context"
	"testing"
)

type fakeRepo struct {
	rows         []OrderRow
	stockLogMap  map[string]StockLogCount
	outboxMap    map[string][]OutboxEventRow
	requeuedIDs  []int64
	listCalls    int
	requeueCalls int
}

func TestRunnerRepairsReservationOnlyOnceOnPaymentSuccessGap(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S116_S50012",
				ReservationID:                 "S116_S50012",
				PaymentID:                     "P50012",
				UserID:                        109,
				ProductID:                     22,
				Quantity:                      1,
				Amount:                        23900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710001100,
				SeckillProductID:              116,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             109,
				ReservationProductID:          22,
				ReservationQuantity:           1,
				ReservationAmount:             23900,
				ReservationStatus:             ReservationStatusPaying,
				PaymentFound:                  true,
				PaymentUserID:                 109,
				PaymentAmount:                 23900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S116_S50012": {
				{ID: 51, EventType: "order.created", Status: OutboxStatusPublished},
				{ID: 52, EventType: "payment.requested", Status: OutboxStatusPublished},
				{ID: 53, EventType: "payment.succeeded", Status: OutboxStatusPublished},
				{ID: 54, EventType: "order.completed", Status: OutboxStatusPublished},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S116_S50012": "success"}}
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

	if len(client.advanceCalls) != 1 {
		t.Fatalf("expected one reservation repair call, got %+v", client.advanceCalls)
	}
	if client.advanceCalls[0].orderID != "S116_S50012" || client.advanceCalls[0].targetStatus != ReservationStatusConsumed {
		t.Fatalf("unexpected advance call: %+v", client.advanceCalls[0])
	}
	if got := sum.RepairableAnomalyCount[AnomalyReservationNotConsumedOnPaymentSuccess]; got != 1 {
		t.Fatalf("expected one repairable reservation anomaly, got %d", got)
	}
}

func TestRunnerMarksDeadOutboxAsManualInterventionForCompletedOrder(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S117_S50013",
				ReservationID:                 "S117_S50013",
				PaymentID:                     "P50013",
				UserID:                        110,
				ProductID:                     23,
				Quantity:                      1,
				Amount:                        24900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710001200,
				SeckillProductID:              117,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             110,
				ReservationProductID:          23,
				ReservationQuantity:           1,
				ReservationAmount:             24900,
				ReservationStatus:             ReservationStatusConsumed,
				PaymentFound:                  true,
				PaymentUserID:                 110,
				PaymentAmount:                 24900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S117_S50013": {
				{ID: 61, EventType: "order.created", Status: OutboxStatusPublished},
				{ID: 62, EventType: "payment.requested", Status: OutboxStatusPublished},
				{ID: 63, EventType: "payment.succeeded", Status: OutboxStatusDead},
				{ID: 64, EventType: "order.completed", Status: OutboxStatusPublished},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S117_S50013": "success"}}
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

	if got := sum.ManualAnomalyCount[AnomalyOutboxDead]; got != 1 {
		t.Fatalf("expected one outbox dead anomaly, got %d", got)
	}
	if len(client.updateCalls) != 0 || len(client.advanceCalls) != 0 || len(client.compensateCalls) != 0 {
		t.Fatalf("expected dead outbox to stay manual-only, got update=%+v advance=%+v compensate=%+v",
			client.updateCalls, client.advanceCalls, client.compensateCalls)
	}
}

func (f *fakeRepo) ListOrders(ctx context.Context, windowStartUnix, windowEndUnix int64, limit, offset int) ([]OrderRow, error) {
	f.listCalls++
	if offset > 0 {
		return nil, nil
	}
	return f.rows, nil
}

func (f *fakeRepo) GetStockLogCounts(ctx context.Context, orderIDs []string) (map[string]StockLogCount, error) {
	if f.stockLogMap == nil {
		return map[string]StockLogCount{}, nil
	}
	return f.stockLogMap, nil
}

func (f *fakeRepo) GetOutboxEvents(ctx context.Context, orderIDs []string) (map[string][]OutboxEventRow, error) {
	if f.outboxMap == nil {
		return map[string][]OutboxEventRow{}, nil
	}
	return f.outboxMap, nil
}

func (f *fakeRepo) RequeueOutbox(ctx context.Context, ids []int64) error {
	f.requeueCalls++
	f.requeuedIDs = append(f.requeuedIDs, ids...)
	return nil
}

type fakeStore struct {
	statuses map[string]string
	locked   bool
}

func (f *fakeStore) AcquireLock(ctx context.Context, key, token string, ttlSeconds int64) (bool, error) {
	f.locked = true
	return true, nil
}

func (f *fakeStore) ReleaseLock(ctx context.Context, key, token string) error {
	f.locked = false
	return nil
}

func (f *fakeStore) GetOrderStatuses(ctx context.Context, orderIDs []string) (map[string]string, error) {
	if f.statuses == nil {
		return map[string]string{}, nil
	}
	return f.statuses, nil
}

type advanceCall struct {
	orderID      string
	targetStatus int32
	reason       string
	allowRecover bool
}

type compensateCall struct {
	orderID          string
	seckillProductID int64
	userID           int64
	quantity         int64
	reason           string
	shardNo          int32
}

type fakeSeckillClient struct {
	updateCalls     []string
	advanceCalls    []advanceCall
	compensateCalls []compensateCall
}

func (f *fakeSeckillClient) UpdateOrderStatus(ctx context.Context, orderID, status string, allowRecover bool) error {
	f.updateCalls = append(f.updateCalls, orderID+":"+status)
	return nil
}

func (f *fakeSeckillClient) CompensateFailedOrder(ctx context.Context, orderID string, seckillProductID, userID, quantity int64, reason string, shardNo int32) (string, error) {
	f.compensateCalls = append(f.compensateCalls, compensateCall{
		orderID:          orderID,
		seckillProductID: seckillProductID,
		userID:           userID,
		quantity:         quantity,
		reason:           reason,
		shardNo:          shardNo,
	})
	return "compensated", nil
}

func (f *fakeSeckillClient) AdvanceReservation(ctx context.Context, orderID string, targetStatus int32, reason string, allowRecover bool) error {
	f.advanceCalls = append(f.advanceCalls, advanceCall{
		orderID:      orderID,
		targetStatus: targetStatus,
		reason:       reason,
		allowRecover: allowRecover,
	})
	return nil
}

func TestRunnerRepairsReservationRedisAndRetryableOutbox(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S105_S50001",
				ReservationID:                 "S105_S50001",
				PaymentID:                     "P50001",
				UserID:                        88,
				ProductID:                     11,
				Quantity:                      1,
				Amount:                        19900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710000000,
				SeckillProductID:              105,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             88,
				ReservationProductID:          11,
				ReservationQuantity:           1,
				ReservationAmount:             19900,
				ReservationStatus:             ReservationStatusPaying,
				PaymentFound:                  true,
				PaymentUserID:                 88,
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
			"S105_S50001": {
				{ID: 11, EventType: "reservation.created", Status: OutboxStatusPublished},
				{ID: 12, EventType: "order.created", Status: OutboxStatusPublished},
				{ID: 13, EventType: "payment.succeeded", Status: OutboxStatusFailed},
				{ID: 14, EventType: "order.completed", Status: OutboxStatusPublished},
			},
		},
	}
	store := &fakeStore{
		statuses: map[string]string{
			"S105_S50001": "pending",
		},
	}
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

	if got := sum.RepairableAnomalyCount[AnomalyReservationNotConsumedOnPaymentSuccess]; got != 1 {
		t.Fatalf("expected reservation repairable anomaly count=1, got %d", got)
	}
	if got := sum.RepairableAnomalyCount[AnomalyRedisNotSuccessOnDBSuccess]; got != 1 {
		t.Fatalf("expected redis repairable anomaly count=1, got %d", got)
	}
	if got := sum.RepairableAnomalyCount[AnomalyOutboxRetryablePending]; got != 1 {
		t.Fatalf("expected outbox repairable anomaly count=1, got %d", got)
	}
	if len(client.advanceCalls) != 1 || client.advanceCalls[0].orderID != "S105_S50001" || client.advanceCalls[0].targetStatus != ReservationStatusConsumed {
		t.Fatalf("unexpected advance reservation calls: %+v", client.advanceCalls)
	}
	if len(client.updateCalls) != 1 || client.updateCalls[0] != "S105_S50001:success" {
		t.Fatalf("unexpected update order status calls: %+v", client.updateCalls)
	}
	if repo.requeueCalls != 1 || len(repo.requeuedIDs) != 1 || repo.requeuedIDs[0] != 13 {
		t.Fatalf("unexpected requeue outbox calls: calls=%d ids=%+v", repo.requeueCalls, repo.requeuedIDs)
	}
	if sum.RepairAttempted != 3 || sum.RepairSucceeded != 3 {
		t.Fatalf("unexpected repair summary: %+v", sum)
	}
}

func TestRunnerFlagsManualAnomaliesWithoutRepair(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                    "S106_S50002",
				ReservationID:              "S106_S50002",
				PaymentID:                  "P50002",
				UserID:                     99,
				ProductID:                  12,
				Quantity:                   1,
				Amount:                     29900,
				Status:                     OrderStatusCompleted,
				PayStatus:                  OrderPayStatusSuccess,
				CreatedAt:                  1710000100,
				SeckillProductID:           106,
				SeckillQuantity:            1,
				ReservationFound:           true,
				ReservationUserID:          99,
				ReservationProductID:       12,
				ReservationQuantity:        1,
				ReservationAmount:          29900,
				ReservationStatus:          ReservationStatusConsumed,
				PaymentFound:               true,
				PaymentUserID:              99,
				PaymentAmount:              39900,
				PaymentStatus:              PaymentStatusSuccess,
				CallbackCount:              1,
				CallbackVerifyFailCount:    1,
				CallbackProcessFailedCount: 1,
				ProcessedMessageFound:      false,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S106_S50002": {
				{ID: 21, EventType: "reservation.created", Status: OutboxStatusPublished},
				{ID: 22, EventType: "payment.succeeded", Status: OutboxStatusDead},
			},
		},
	}
	store := &fakeStore{
		statuses: map[string]string{
			"S106_S50002": "success",
		},
	}
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

	if got := sum.ManualAnomalyCount[AnomalyPaymentAmountMismatch]; got != 1 {
		t.Fatalf("expected payment amount mismatch anomaly count=1, got %d", got)
	}
	if got := sum.ManualAnomalyCount[AnomalyCallbackVerifyFailedOnPaymentSuccess]; got != 1 {
		t.Fatalf("expected callback verify failed anomaly count=1, got %d", got)
	}
	if got := sum.ManualAnomalyCount[AnomalyOutboxDead]; got != 1 {
		t.Fatalf("expected outbox dead anomaly count=1, got %d", got)
	}
	if got := sum.ManualAnomalyCount[AnomalyProcessedMessageMissing]; got != 1 {
		t.Fatalf("expected processed message missing anomaly count=1, got %d", got)
	}
	if len(client.advanceCalls) != 0 || len(client.updateCalls) != 0 || len(client.compensateCalls) != 0 {
		t.Fatalf("expected no repair calls, got advance=%+v update=%+v compensate=%+v", client.advanceCalls, client.updateCalls, client.compensateCalls)
	}
	if repo.requeueCalls != 0 || sum.RepairAttempted != 0 {
		t.Fatalf("expected no outbox repair, got calls=%d summary=%+v", repo.requeueCalls, sum)
	}
}

func TestRunnerDoesNotTreatNewOutboxAsRepairableAnomaly(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S107_S50003",
				ReservationID:                 "S107_S50003",
				PaymentID:                     "P50003",
				UserID:                        100,
				ProductID:                     13,
				Quantity:                      1,
				Amount:                        10900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710000200,
				SeckillProductID:              107,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             100,
				ReservationProductID:          13,
				ReservationQuantity:           1,
				ReservationAmount:             10900,
				ReservationStatus:             ReservationStatusConsumed,
				PaymentFound:                  true,
				PaymentUserID:                 100,
				PaymentAmount:                 10900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S107_S50003": {
				{ID: 31, EventType: "order.created", Status: OutboxStatusPublished},
				{ID: 32, EventType: "payment.succeeded", Status: OutboxStatusPublished},
				{ID: 33, EventType: "order.completed", Status: OutboxStatusNew},
			},
		},
	}
	store := &fakeStore{
		statuses: map[string]string{
			"S107_S50003": "success",
		},
	}
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

	if got := sum.RepairableAnomalyCount[AnomalyOutboxRetryablePending]; got != 0 {
		t.Fatalf("expected no retryable outbox anomaly for new status, got %d", got)
	}
	if repo.requeueCalls != 0 || sum.RepairAttempted != 0 {
		t.Fatalf("expected no requeue for new outbox status, got calls=%d summary=%+v", repo.requeueCalls, sum)
	}
}

func TestRunnerDoesNotFlagEventOrderWhenPrimaryChainIsOrdered(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                       "S115_S50011",
				ReservationID:                 "S115_S50011",
				PaymentID:                     "P50011",
				UserID:                        108,
				ProductID:                     21,
				Quantity:                      1,
				Amount:                        22900,
				Status:                        OrderStatusCompleted,
				PayStatus:                     OrderPayStatusSuccess,
				CreatedAt:                     1710001000,
				SeckillProductID:              115,
				SeckillQuantity:               1,
				ReservationFound:              true,
				ReservationUserID:             108,
				ReservationProductID:          21,
				ReservationQuantity:           1,
				ReservationAmount:             22900,
				ReservationStatus:             ReservationStatusConsumed,
				PaymentFound:                  true,
				PaymentUserID:                 108,
				PaymentAmount:                 22900,
				PaymentStatus:                 PaymentStatusSuccess,
				CallbackCount:                 1,
				CallbackVerifyPassCount:       1,
				CallbackProcessSucceededCount: 1,
				ProcessedMessageFound:         true,
				ProcessedMessageStatus:        ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S115_S50011": {
				{ID: 41, EventType: "order.created", Status: OutboxStatusPublished},
				{ID: 42, EventType: "payment.requested", Status: OutboxStatusPublished},
				{ID: 43, EventType: "payment.succeeded", Status: OutboxStatusPublished},
				{ID: 44, EventType: "order.completed", Status: OutboxStatusPublished},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S115_S50011": "success"}}
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

	if got := sum.ManualAnomalyCount[AnomalyOutboxEventOrderInvalid]; got != 0 {
		t.Fatalf("expected no event order invalid anomaly, got %d", got)
	}
}

func TestRunnerFlagsMissingShardNoInStrictOutboxPayload(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                "S118_S50014",
				ReservationID:          "S118_S50014",
				UserID:                 111,
				ProductID:              24,
				Quantity:               1,
				Amount:                 25900,
				Status:                 OrderStatusOrderCreated,
				PayStatus:              OrderPayStatusInit,
				CreatedAt:              1710001300,
				SeckillProductID:       118,
				SeckillQuantity:        1,
				ReservationFound:       true,
				ReservationUserID:      111,
				ReservationProductID:   24,
				ReservationQuantity:    1,
				ReservationAmount:      25900,
				ReservationStatus:      ReservationStatusOrderCreated,
				ProcessedMessageFound:  true,
				ProcessedMessageStatus: ProcessedMessageStatusSucceeded,
			},
		},
		outboxMap: map[string][]OutboxEventRow{
			"S118_S50014": {
				{
					ID:        71,
					EventType: "order.created",
					Status:    OutboxStatusPublished,
					PayloadJSON: `{
						"event_id":"evt-71",
						"event_type":"order.created",
						"occurred_at":1710001300,
						"aggregate_type":"order",
						"aggregate_id":"S118_S50014",
						"trace_id":"trace-71",
						"source":"order-service",
						"version":1,
						"message_id":"S118_S50014",
						"reservation_id":"S118_S50014",
						"order_id":"S118_S50014",
						"user_id":111,
						"seckill_product_id":118,
						"product_id":24,
						"quantity":1,
						"amount":25900,
						"status":2,
						"order_type":1,
						"pay_status":0
					}`,
				},
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S118_S50014": "pending"}}
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
		t.Fatalf("expected one outbox payload missing fields anomaly, got %d", got)
	}
}

func TestRunnerFlagsShardMismatchBetweenOrderAndReservation(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                "S119_S50015",
				ReservationID:          "S119_S50015",
				UserID:                 112,
				ProductID:              25,
				Quantity:               1,
				Amount:                 26900,
				ShardNo:                3,
				Status:                 OrderStatusOrderCreated,
				PayStatus:              OrderPayStatusInit,
				CreatedAt:              1710001400,
				SeckillProductID:       119,
				SeckillQuantity:        1,
				SeckillShardNo:         3,
				ReservationFound:       true,
				ReservationUserID:      112,
				ReservationProductID:   25,
				ReservationQuantity:    1,
				ReservationAmount:      26900,
				ReservationShardNo:     4,
				ReservationStatus:      ReservationStatusOrderCreated,
				ProcessedMessageFound:  true,
				ProcessedMessageStatus: ProcessedMessageStatusSucceeded,
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S119_S50015": "pending"}}
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

	if got := sum.ManualAnomalyCount[AnomalyShardMismatch]; got != 1 {
		t.Fatalf("expected one shard mismatch anomaly, got %d", got)
	}
}

func TestRunnerSkipsCompensationWhenReservationShardMissing(t *testing.T) {
	repo := &fakeRepo{
		rows: []OrderRow{
			{
				OrderID:                "S120_S50016",
				ReservationID:          "S120_S50016",
				UserID:                 113,
				ProductID:              26,
				Quantity:               1,
				Amount:                 27900,
				ShardNo:                2,
				Status:                 OrderStatusFailed,
				PayStatus:              OrderPayStatusInit,
				CreatedAt:              1710001500,
				SeckillProductID:       120,
				SeckillQuantity:        1,
				SeckillShardNo:         2,
				ReservationFound:       true,
				ReservationUserID:      113,
				ReservationProductID:   26,
				ReservationQuantity:    1,
				ReservationAmount:      27900,
				ReservationShardNo:     0,
				ReservationStatus:      ReservationStatusFailed,
				ProcessedMessageFound:  true,
				ProcessedMessageStatus: ProcessedMessageStatusSucceeded,
			},
		},
	}
	store := &fakeStore{statuses: map[string]string{"S120_S50016": "pending"}}
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

	if len(client.compensateCalls) != 0 {
		t.Fatalf("expected compensation to be skipped, got %+v", client.compensateCalls)
	}
	if got := sum.ManualAnomalyCount[AnomalyCompensationShardMissing]; got != 1 {
		t.Fatalf("expected one compensation shard missing anomaly, got %d", got)
	}
}
