package reconcile

import "context"

const (
	RedisStatusMissing = "missing"

	OrderStatusPending   int32 = 0
	OrderStatusPaid      int32 = 1
	OrderStatusCancelled int32 = 2
	OrderStatusRefunded  int32 = 3
	OrderStatusCompleted int32 = 4
)

const (
	AnomalyMissingSeckillOrderRecord    = "missing_seckill_order_record"
	AnomalyRedisNotSuccessOnDBSuccess   = "redis_not_success_on_db_success"
	AnomalyRedisNotFailedOnDBFailed     = "redis_not_failed_on_db_failed"
	AnomalyStockRollbackWithoutDeduct   = "stocklog_rollback_without_deduct"
	AnomalyStockRollbackButOrderSuccess = "stocklog_rollback_but_order_success"
	AnomalyStockMissingRollbackOnFailed = "stocklog_missing_rollback_on_failed"
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
	UserID           int64
	ProductID        int64
	Quantity         int64
	Status           int32
	CreatedAt        int64
	SeckillProductID int64
	SeckillQuantity  int64
}

type StockLogCount struct {
	DeductCount   int64
	RollbackCount int64
}

type Repo interface {
	ListOrders(ctx context.Context, windowStartUnix, windowEndUnix int64, limit, offset int) ([]OrderRow, error)
	GetStockLogCounts(ctx context.Context, orderIDs []string) (map[string]StockLogCount, error)
}

type Store interface {
	AcquireLock(ctx context.Context, key, token string, ttlSeconds int64) (bool, error)
	ReleaseLock(ctx context.Context, key, token string) error
	GetOrderStatuses(ctx context.Context, orderIDs []string) (map[string]string, error)
}

type SeckillClient interface {
	UpdateOrderStatus(ctx context.Context, orderID, status string, allowRecover bool) error
	CompensateFailedOrder(ctx context.Context, orderID string, seckillProductID, userID, quantity int64, reason string) (string, error)
}

type Summary struct {
	Locked             bool
	ScannedOrders      int
	AnomalyCount       map[string]int
	RepairAttempted    int
	RepairSucceeded    int
	RepairFailed       int
	RepairSkippedLimit int
}
