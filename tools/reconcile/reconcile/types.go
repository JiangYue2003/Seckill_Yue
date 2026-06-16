package reconcile

import "context"

const (
	RedisStatusMissing = "missing"

	OrderStatusInit         int32 = 0
	OrderStatusReserved     int32 = 1
	OrderStatusOrderCreated int32 = 2
	OrderStatusPaying       int32 = 3
	OrderStatusPaid         int32 = 4
	OrderStatusCompleted    int32 = 5
	OrderStatusCancelled    int32 = 6
	OrderStatusExpired      int32 = 7
	OrderStatusFailed       int32 = 8
	OrderStatusRefunded     int32 = 9
)

const (
	OrderPayStatusInit      int32 = 0
	OrderPayStatusRequested int32 = 1
	OrderPayStatusSuccess   int32 = 2
	OrderPayStatusFailed    int32 = 3
	OrderPayStatusClosed    int32 = 4
	OrderPayStatusRefunded  int32 = 5
)

const (
	ReservationStatusReserved      int32 = 0
	ReservationStatusOrderCreating int32 = 1
	ReservationStatusOrderCreated  int32 = 2
	ReservationStatusPaying        int32 = 3
	ReservationStatusPaid          int32 = 4
	ReservationStatusConsumed      int32 = 5
	ReservationStatusReleased      int32 = 6
	ReservationStatusExpired       int32 = 7
	ReservationStatusFailed        int32 = 8
)

const (
	PaymentStatusInit      int32 = 0
	PaymentStatusRequested int32 = 1
	PaymentStatusSuccess   int32 = 2
	PaymentStatusFailed    int32 = 3
	PaymentStatusClosed    int32 = 4
	PaymentStatusRefunded  int32 = 5
)

const (
	PaymentCallbackVerifyUnknown int32 = 0
	PaymentCallbackVerifyPass    int32 = 1
	PaymentCallbackVerifyFail    int32 = 2
)

const (
	PaymentCallbackProcessInit      int32 = 0
	PaymentCallbackProcessSucceeded int32 = 1
	PaymentCallbackProcessFailed    int32 = 2
	PaymentCallbackProcessDuplicate int32 = 3
)

const (
	ProcessedMessageStatusProcessing int32 = 0
	ProcessedMessageStatusSucceeded  int32 = 1
	ProcessedMessageStatusFailed     int32 = 2
)

const (
	OutboxStatusNew           int32 = 0
	OutboxStatusPublished     int32 = 1
	OutboxStatusConsumedAcked int32 = 2
	OutboxStatusFailed        int32 = 3
	OutboxStatusDead          int32 = 4
)

const (
	AnomalyMissingSeckillOrderRecord              = "missing_seckill_order_record"
	AnomalyProcessedMessageMissing                = "processed_message_missing"
	AnomalyProcessedMessageNotSucceeded           = "processed_message_not_succeeded"
	AnomalyReservationMissing                     = "reservation_missing"
	AnomalyReservationUserMismatch                = "reservation_user_mismatch"
	AnomalyReservationProductMismatch             = "reservation_product_mismatch"
	AnomalyReservationQuantityMismatch            = "reservation_quantity_mismatch"
	AnomalyReservationAmountMismatch              = "reservation_amount_mismatch"
	AnomalyReservationNotConsumedOnPaymentSuccess = "reservation_not_consumed_on_payment_success"
	AnomalyReservationMissingOrderCreated         = "reservation_missing_order_created"
	AnomalyPaymentMissing                         = "payment_missing"
	AnomalyPaymentUserMismatch                    = "payment_user_mismatch"
	AnomalyPaymentAmountMismatch                  = "payment_amount_mismatch"
	AnomalyPaymentStatusMismatch                  = "payment_status_mismatch"
	AnomalyCallbackMissingOnPaymentSuccess        = "callback_missing_on_payment_success"
	AnomalyCallbackVerifyFailedOnPaymentSuccess   = "callback_verify_failed_on_payment_success"
	AnomalyCallbackProcessFailedOnPaymentSuccess  = "callback_process_failed_on_payment_success"
	AnomalyOutboxMissingOrderCreated              = "outbox_missing_order_created"
	AnomalyOutboxMissingPaymentSucceeded          = "outbox_missing_payment_succeeded"
	AnomalyOutboxMissingOrderCompleted            = "outbox_missing_order_completed"
	AnomalyOutboxRetryablePending                 = "outbox_retryable_pending"
	AnomalyOutboxDead                             = "outbox_dead"
	AnomalyRedisNotSuccessOnDBSuccess             = "redis_not_success_on_db_success"
	AnomalyRedisNotFailedOnDBFailed               = "redis_not_failed_on_db_failed"
	AnomalyStockRollbackWithoutDeduct             = "stocklog_rollback_without_deduct"
	AnomalyStockRollbackButOrderSuccess           = "stocklog_rollback_but_order_success"
	AnomalyStockMissingRollbackOnFailed           = "stocklog_missing_rollback_on_failed"
)

type Config struct {
	WindowStartUnix int64
	WindowEndUnix   int64
	BatchSize       int
	DryRun          bool
	MaxRepair       int
	LockKey         string
	LockTTLSeconds  int64
}

type OrderRow struct {
	OrderID          string
	ReservationID    string
	PaymentID        string
	UserID           int64
	ProductID        int64
	Quantity         int64
	Amount           int64
	Status           int32
	PayStatus        int32
	CreatedAt        int64
	SeckillProductID int64
	SeckillQuantity  int64

	ReservationFound     bool
	ReservationUserID    int64
	ReservationProductID int64
	ReservationQuantity  int64
	ReservationAmount    int64
	ReservationStatus    int32

	PaymentFound  bool
	PaymentUserID int64
	PaymentAmount int64
	PaymentStatus int32

	CallbackCount                 int64
	CallbackVerifyPassCount       int64
	CallbackVerifyFailCount       int64
	CallbackProcessSucceededCount int64
	CallbackProcessFailedCount    int64
	CallbackProcessDuplicateCount int64

	ProcessedMessageFound  bool
	ProcessedMessageStatus int32
}

type StockLogCount struct {
	DeductCount   int64
	RollbackCount int64
}

type OutboxEventRow struct {
	ID        int64
	EventType string
	Status    int32
}

type Repo interface {
	ListOrders(ctx context.Context, windowStartUnix, windowEndUnix int64, limit, offset int) ([]OrderRow, error)
	GetStockLogCounts(ctx context.Context, orderIDs []string) (map[string]StockLogCount, error)
	GetOutboxEvents(ctx context.Context, orderIDs []string) (map[string][]OutboxEventRow, error)
	RequeueOutbox(ctx context.Context, ids []int64) error
}

type Store interface {
	AcquireLock(ctx context.Context, key, token string, ttlSeconds int64) (bool, error)
	ReleaseLock(ctx context.Context, key, token string) error
	GetOrderStatuses(ctx context.Context, orderIDs []string) (map[string]string, error)
}

type SeckillClient interface {
	UpdateOrderStatus(ctx context.Context, orderID, status string, allowRecover bool) error
	CompensateFailedOrder(ctx context.Context, orderID string, seckillProductID, userID, quantity int64, reason string) (string, error)
	AdvanceReservation(ctx context.Context, orderID string, targetStatus int32, reason string, allowRecover bool) error
}

type Summary struct {
	Locked                 bool
	ScannedOrders          int
	RepairableAnomalyCount map[string]int
	ManualAnomalyCount     map[string]int
	RepairAttempted        int
	RepairSucceeded        int
	RepairFailed           int
	RepairSkippedLimit     int
}
