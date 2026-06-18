package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"
)

const (
	reconcileReasonDBFailed          = "reconcile_db_failed_status"
	reconcileReasonPaymentSuccessFix = "reconcile_payment_success_fix"
	reconcileReasonRetryOutbox       = "reconcile_retry_outbox"
)

type Runner struct {
	cfg     Config
	repo    Repo
	store   Store
	client  SeckillClient
	logf    func(format string, args ...any)
	lockVal string
}

func NewRunner(cfg Config, repo Repo, store Store, client SeckillClient, logf func(format string, args ...any)) (*Runner, error) {
	if repo == nil || store == nil || client == nil {
		return nil, fmt.Errorf("repo/store/client must not be nil")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 500
	}
	if cfg.MaxRepair <= 0 {
		cfg.MaxRepair = 200
	}
	if cfg.LockKey == "" {
		cfg.LockKey = "reconcile:seckill:lock"
	}
	if cfg.LockTTLSeconds <= 0 {
		cfg.LockTTLSeconds = 120
	}
	if cfg.WindowEndUnix <= cfg.WindowStartUnix {
		return nil, fmt.Errorf("invalid time window")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}

	return &Runner{
		cfg:    cfg,
		repo:   repo,
		store:  store,
		client: client,
		logf:   logf,
		lockVal: fmt.Sprintf(
			"reconcile-%d-%d",
			time.Now().UnixNano(),
			rand.Int63(), //nolint:gosec
		),
	}, nil
}

func (r *Runner) Run(ctx context.Context) (Summary, error) {
	sum := Summary{
		RepairableAnomalyCount: make(map[string]int),
		ManualAnomalyCount:     make(map[string]int),
	}

	locked, err := r.store.AcquireLock(ctx, r.cfg.LockKey, r.lockVal, r.cfg.LockTTLSeconds)
	if err != nil {
		return sum, fmt.Errorf("acquire lock failed: %w", err)
	}
	if !locked {
		r.logf("level=info msg=\"lock not acquired, another reconcile is running\" lock_key=%s", r.cfg.LockKey)
		return sum, nil
	}
	sum.Locked = true
	defer func() {
		if releaseErr := r.store.ReleaseLock(context.Background(), r.cfg.LockKey, r.lockVal); releaseErr != nil {
			r.logf("level=warn msg=\"release lock failed\" lock_key=%s err=%v", r.cfg.LockKey, releaseErr)
		}
	}()

	offset := 0
	for {
		rows, err := r.repo.ListOrders(ctx, r.cfg.WindowStartUnix, r.cfg.WindowEndUnix, r.cfg.BatchSize, offset)
		if err != nil {
			return sum, fmt.Errorf("list orders failed: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		sum.ScannedOrders += len(rows)

		orderIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			orderIDs = append(orderIDs, row.OrderID)
		}

		redisStatuses, err := r.store.GetOrderStatuses(ctx, orderIDs)
		if err != nil {
			return sum, fmt.Errorf("get redis statuses failed: %w", err)
		}

		stockLogMap, err := r.repo.GetStockLogCounts(ctx, orderIDs)
		if err != nil {
			return sum, fmt.Errorf("get stock logs failed: %w", err)
		}

		outboxMap, err := r.repo.GetOutboxEvents(ctx, orderIDs)
		if err != nil {
			return sum, fmt.Errorf("get outbox events failed: %w", err)
		}

		for _, row := range rows {
			redisStatus := redisStatuses[row.OrderID]
			if redisStatus == "" {
				redisStatus = RedisStatusMissing
			}
			stock := stockLogMap[row.OrderID]
			outboxes := outboxMap[row.OrderID]

			r.checkProcessedMessage(&sum, row)
			r.checkReservationConsistency(ctx, &sum, row)
			r.checkPaymentConsistency(&sum, row)
			r.checkCallbackConsistency(&sum, row)
			if err := r.checkOutboxConsistency(ctx, &sum, row, outboxes); err != nil {
				return sum, err
			}
			r.checkStockConsistency(&sum, row, stock)
			if err := r.checkRedisConsistency(ctx, &sum, row, redisStatus); err != nil {
				return sum, err
			}
		}

		offset += len(rows)
	}

	return sum, nil
}

func (r *Runner) checkProcessedMessage(sum *Summary, row OrderRow) {
	if !row.ProcessedMessageFound {
		r.markManualAnomaly(sum, AnomalyProcessedMessageMissing, row.OrderID)
		return
	}
	if row.ProcessedMessageStatus != ProcessedMessageStatusSucceeded {
		r.markManualAnomaly(sum, AnomalyProcessedMessageNotSucceeded, row.OrderID)
	}
}

func (r *Runner) checkReservationConsistency(ctx context.Context, sum *Summary, row OrderRow) {
	if row.SeckillProductID <= 0 {
		r.markManualAnomaly(sum, AnomalyMissingSeckillOrderRecord, row.OrderID)
	}
	if row.ShardNo <= 0 {
		r.markManualAnomaly(sum, AnomalyOrderShardMissing, row.OrderID)
	}
	if row.SeckillProductID > 0 && row.SeckillShardNo <= 0 {
		r.markManualAnomaly(sum, AnomalySeckillOrderShardMissing, row.OrderID)
	}
	if !row.ReservationFound {
		r.markManualAnomaly(sum, AnomalyReservationMissing, row.OrderID)
		return
	}
	if row.ReservationShardNo <= 0 {
		r.markManualAnomaly(sum, AnomalyReservationShardMissing, row.OrderID)
	} else if row.ShardNo > 0 && row.ShardNo != row.ReservationShardNo {
		r.markManualAnomaly(sum, AnomalyShardMismatch, row.OrderID)
	} else if row.SeckillShardNo > 0 && row.SeckillShardNo != row.ReservationShardNo {
		r.markManualAnomaly(sum, AnomalyShardMismatch, row.OrderID)
	}
	if row.ShardNo > 0 && row.SeckillShardNo > 0 && row.ShardNo != row.SeckillShardNo {
		r.markManualAnomaly(sum, AnomalyShardMismatch, row.OrderID)
	}
	if row.ReservationUserID != 0 && row.ReservationUserID != row.UserID {
		r.markManualAnomaly(sum, AnomalyReservationUserMismatch, row.OrderID)
	}
	if row.ReservationProductID != 0 && row.ReservationProductID != row.ProductID {
		r.markManualAnomaly(sum, AnomalyReservationProductMismatch, row.OrderID)
	}
	if row.ReservationQuantity != 0 && row.ReservationQuantity != row.Quantity {
		r.markManualAnomaly(sum, AnomalyReservationQuantityMismatch, row.OrderID)
	}
	if row.ReservationAmount != 0 && row.ReservationAmount != row.Amount {
		r.markManualAnomaly(sum, AnomalyReservationAmountMismatch, row.OrderID)
	}

	if row.Status >= OrderStatusOrderCreated && row.ReservationStatus < ReservationStatusOrderCreated {
		r.markManualAnomaly(sum, AnomalyReservationMissingOrderCreated, row.OrderID)
	}
	if hasPaymentSuccess(row) && row.ReservationStatus < ReservationStatusConsumed {
		r.markRepairableAnomaly(sum, AnomalyReservationNotConsumedOnPaymentSuccess, row.OrderID)
		r.doRepair(ctx, sum, func(ctx context.Context) error {
			return r.client.AdvanceReservation(ctx, row.OrderID, ReservationStatusConsumed, reconcileReasonPaymentSuccessFix, true)
		})
	}
}

func (r *Runner) checkPaymentConsistency(sum *Summary, row OrderRow) {
	if expectsPayment(row) && !row.PaymentFound {
		r.markManualAnomaly(sum, AnomalyPaymentMissing, row.OrderID)
		return
	}
	if !row.PaymentFound {
		return
	}
	if row.PaymentUserID != 0 && row.PaymentUserID != row.UserID {
		r.markManualAnomaly(sum, AnomalyPaymentUserMismatch, row.OrderID)
	}
	if row.PaymentAmount != 0 && row.PaymentAmount != row.Amount {
		r.markManualAnomaly(sum, AnomalyPaymentAmountMismatch, row.OrderID)
	}
	if hasPaymentSuccess(row) && row.PaymentStatus != PaymentStatusSuccess {
		r.markManualAnomaly(sum, AnomalyPaymentStatusMismatch, row.OrderID)
	}
}

func (r *Runner) checkCallbackConsistency(sum *Summary, row OrderRow) {
	if !row.PaymentFound || row.PaymentStatus != PaymentStatusSuccess {
		return
	}
	if row.CallbackCount == 0 {
		r.markManualAnomaly(sum, AnomalyCallbackMissingOnPaymentSuccess, row.OrderID)
	}
	if row.CallbackVerifyFailCount > 0 {
		r.markManualAnomaly(sum, AnomalyCallbackVerifyFailedOnPaymentSuccess, row.OrderID)
	}
	if row.CallbackProcessFailedCount > 0 {
		r.markManualAnomaly(sum, AnomalyCallbackProcessFailedOnPaymentSuccess, row.OrderID)
	}
}

func (r *Runner) checkOutboxConsistency(ctx context.Context, sum *Summary, row OrderRow, outboxes []OutboxEventRow) error {
	seenOrderCreated := false
	seenPaymentRequested := false
	seenPaymentSucceeded := false
	seenOrderCompleted := false
	retryableIDs := make([]int64, 0)
	orderCreatedID := int64(0)
	paymentRequestedID := int64(0)
	paymentSucceededID := int64(0)
	orderCompletedID := int64(0)

	for _, outbox := range outboxes {
		switch outbox.EventType {
		case "order.created":
			seenOrderCreated = true
			if orderCreatedID == 0 {
				orderCreatedID = outbox.ID
			}
		case "payment.requested":
			seenPaymentRequested = true
			if paymentRequestedID == 0 {
				paymentRequestedID = outbox.ID
			}
		case "payment.succeeded":
			seenPaymentSucceeded = true
			if paymentSucceededID == 0 {
				paymentSucceededID = outbox.ID
			}
		case "order.completed":
			seenOrderCompleted = true
			if orderCompletedID == 0 {
				orderCompletedID = outbox.ID
			}
		}

		if requiresStrictPayloadContract(outbox.EventType) && !hasRequiredOutboxFields(outbox) {
			r.markManualAnomaly(sum, AnomalyOutboxPayloadMissingFields, row.OrderID)
		}

		if outbox.Status == OutboxStatusFailed {
			r.markRepairableAnomaly(sum, AnomalyOutboxRetryablePending, row.OrderID)
			retryableIDs = append(retryableIDs, outbox.ID)
			continue
		}
		if outbox.Status == OutboxStatusDead {
			r.markManualAnomaly(sum, AnomalyOutboxDead, row.OrderID)
		}
	}

	if row.Status >= OrderStatusOrderCreated && !seenOrderCreated {
		r.markManualAnomaly(sum, AnomalyOutboxMissingOrderCreated, row.OrderID)
	}
	if expectsPayment(row) && !seenPaymentRequested {
		r.markManualAnomaly(sum, AnomalyOutboxMissingPaymentRequested, row.OrderID)
	}
	if hasPaymentSuccess(row) && !seenPaymentSucceeded {
		r.markManualAnomaly(sum, AnomalyOutboxMissingPaymentSucceeded, row.OrderID)
	}
	if row.Status == OrderStatusCompleted && !seenOrderCompleted {
		r.markManualAnomaly(sum, AnomalyOutboxMissingOrderCompleted, row.OrderID)
	}
	if violatesPrimaryEventOrder(orderCreatedID, paymentRequestedID, paymentSucceededID, orderCompletedID) {
		r.markManualAnomaly(sum, AnomalyOutboxEventOrderInvalid, row.OrderID)
	}

	if len(retryableIDs) > 0 {
		return r.doRepair(ctx, sum, func(ctx context.Context) error {
			return r.repo.RequeueOutbox(ctx, retryableIDs)
		})
	}
	return nil
}

func (r *Runner) checkRedisConsistency(ctx context.Context, sum *Summary, row OrderRow, redisStatus string) error {
	if isDBSuccess(row.Status) && redisStatus != "success" {
		r.markRepairableAnomaly(sum, AnomalyRedisNotSuccessOnDBSuccess, row.OrderID)
		return r.doRepair(ctx, sum, func(ctx context.Context) error {
			return r.client.UpdateOrderStatus(ctx, row.OrderID, "success", true)
		})
	}

	if isDBFailed(row.Status) && (redisStatus == "pending" || redisStatus == "success") {
		r.markRepairableAnomaly(sum, AnomalyRedisNotFailedOnDBFailed, row.OrderID)
		quantity := row.SeckillQuantity
		if quantity <= 0 {
			quantity = row.Quantity
		}
		if row.SeckillProductID <= 0 || row.UserID <= 0 || quantity <= 0 {
			r.logf(
				"level=warn msg=\"skip failed compensation due to incomplete fields\" order_id=%s seckill_product_id=%d user_id=%d quantity=%d",
				row.OrderID, row.SeckillProductID, row.UserID, quantity,
			)
			return nil
		}
		if row.ReservationShardNo <= 0 {
			r.markManualAnomaly(sum, AnomalyCompensationShardMissing, row.OrderID)
			r.logf(
				"level=warn msg=\"skip failed compensation due to missing shard\" order_id=%s seckill_product_id=%d user_id=%d quantity=%d",
				row.OrderID, row.SeckillProductID, row.UserID, quantity,
			)
			return nil
		}

		return r.doRepair(ctx, sum, func(ctx context.Context) error {
			_, err := r.client.CompensateFailedOrder(
				ctx,
				row.OrderID,
				row.SeckillProductID,
				row.UserID,
				quantity,
				reconcileReasonDBFailed,
				row.ReservationShardNo,
			)
			return err
		})
	}
	return nil
}

func (r *Runner) checkStockConsistency(sum *Summary, row OrderRow, stock StockLogCount) {
	if stock.RollbackCount > 0 && stock.DeductCount == 0 {
		r.markManualAnomaly(sum, AnomalyStockRollbackWithoutDeduct, row.OrderID)
	}
	if isDBSuccess(row.Status) && stock.RollbackCount > 0 && stock.DeductCount > 0 {
		r.markManualAnomaly(sum, AnomalyStockRollbackButOrderSuccess, row.OrderID)
	}
	if isDBFailed(row.Status) && stock.DeductCount > 0 && stock.RollbackCount == 0 {
		r.markManualAnomaly(sum, AnomalyStockMissingRollbackOnFailed, row.OrderID)
	}
}

func (r *Runner) doRepair(ctx context.Context, sum *Summary, fn func(context.Context) error) error {
	if r.cfg.DryRun {
		return nil
	}
	if sum.RepairAttempted >= r.cfg.MaxRepair {
		sum.RepairSkippedLimit++
		return nil
	}

	sum.RepairAttempted++
	if err := fn(ctx); err != nil {
		sum.RepairFailed++
		r.logf("level=error msg=\"repair failed\" err=%v", err)
		return err
	}
	sum.RepairSucceeded++
	return nil
}

func (r *Runner) markRepairableAnomaly(sum *Summary, anomalyType, orderID string) {
	sum.RepairableAnomalyCount[anomalyType]++
	r.logf("level=warn msg=\"repairable anomaly found\" order_id=%s type=%s", orderID, anomalyType)
}

func (r *Runner) markManualAnomaly(sum *Summary, anomalyType, orderID string) {
	sum.ManualAnomalyCount[anomalyType]++
	r.logf("level=warn msg=\"manual anomaly found\" order_id=%s type=%s", orderID, anomalyType)
}

func isDBSuccess(status int32) bool {
	return status == OrderStatusPaid || status == OrderStatusCompleted
}

func isDBFailed(status int32) bool {
	return status == OrderStatusCancelled || status == OrderStatusExpired || status == OrderStatusFailed || status == OrderStatusRefunded
}

func expectsPayment(row OrderRow) bool {
	return row.Status >= OrderStatusPaying || row.PayStatus > OrderPayStatusInit || row.PaymentID != ""
}

func hasPaymentSuccess(row OrderRow) bool {
	return row.PayStatus == OrderPayStatusSuccess || row.PaymentStatus == PaymentStatusSuccess || row.Status == OrderStatusPaid || row.Status == OrderStatusCompleted
}

func requiresStrictPayloadContract(eventType string) bool {
	switch eventType {
	case "reservation.created", "reservation.released", "reservation.advanced", "order.created", "payment.requested", "payment.succeeded", "order.completed":
		return true
	default:
		return false
	}
}

func hasRequiredOutboxFields(outbox OutboxEventRow) bool {
	if outbox.PayloadJSON == "" {
		return false
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(outbox.PayloadJSON), &payload); err != nil {
		return false
	}

	required := []string{
		"event_id",
		"event_type",
		"occurred_at",
		"aggregate_type",
		"aggregate_id",
		"trace_id",
		"source",
		"version",
	}

	switch outbox.EventType {
	case "reservation.created":
		required = append(required,
			"message_id",
			"reservation_id",
			"order_id",
			"user_id",
			"seckill_product_id",
			"product_id",
			"quantity",
			"amount",
			"shard_no",
			"status",
			"expire_at",
		)
	case "reservation.released":
		required = append(required,
			"message_id",
			"reservation_id",
			"order_id",
			"user_id",
			"seckill_product_id",
			"product_id",
			"quantity",
			"amount",
			"shard_no",
			"from_status",
			"status",
			"reason",
		)
	case "reservation.advanced":
		required = append(required,
			"message_id",
			"reservation_id",
			"order_id",
			"payment_id",
			"user_id",
			"seckill_product_id",
			"product_id",
			"quantity",
			"amount",
			"shard_no",
			"from_status",
			"status",
			"reason",
		)
	case "order.created":
		required = append(required,
			"message_id",
			"reservation_id",
			"order_id",
			"user_id",
			"seckill_product_id",
			"product_id",
			"quantity",
			"amount",
			"shard_no",
			"status",
			"order_type",
			"pay_status",
		)
	case "payment.requested":
		required = append(required,
			"message_id",
			"reservation_id",
			"order_id",
			"payment_id",
			"user_id",
			"seckill_product_id",
			"product_id",
			"quantity",
			"amount",
			"shard_no",
			"status",
			"channel",
		)
	case "payment.succeeded":
		required = append(required,
			"message_id",
			"reservation_id",
			"order_id",
			"payment_id",
			"user_id",
			"seckill_product_id",
			"product_id",
			"quantity",
			"amount",
			"shard_no",
			"status",
			"channel",
			"third_party_trade_no",
			"paid_at",
			"callback_id",
		)
	case "order.completed":
		required = append(required,
			"message_id",
			"reservation_id",
			"order_id",
			"payment_id",
			"user_id",
			"seckill_product_id",
			"product_id",
			"quantity",
			"amount",
			"shard_no",
			"status",
			"order_type",
			"pay_status",
			"paid_at",
		)
	}

	for _, key := range required {
		if _, ok := payload[key]; !ok {
			return false
		}
	}
	return true
}

func violatesPrimaryEventOrder(orderCreatedID, paymentRequestedID, paymentSucceededID, orderCompletedID int64) bool {
	if orderCreatedID > 0 && paymentRequestedID > 0 && orderCreatedID >= paymentRequestedID {
		return true
	}
	if paymentRequestedID > 0 && paymentSucceededID > 0 && paymentRequestedID >= paymentSucceededID {
		return true
	}
	if paymentSucceededID > 0 && orderCompletedID > 0 && paymentSucceededID >= orderCompletedID {
		return true
	}
	return false
}
