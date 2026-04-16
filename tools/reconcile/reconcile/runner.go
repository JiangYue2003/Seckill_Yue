package reconcile

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

const (
	reconcileReasonDBFailed = "reconcile_db_failed_status"
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

	r := &Runner{
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
	}
	return r, nil
}

func (r *Runner) Run(ctx context.Context) (Summary, error) {
	sum := Summary{
		AnomalyCount: make(map[string]int),
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

		for _, row := range rows {
			redisStatus := redisStatuses[row.OrderID]
			if redisStatus == "" {
				redisStatus = RedisStatusMissing
			}
			stock := stockLogMap[row.OrderID]

			if row.SeckillProductID <= 0 {
				r.markAnomaly(&sum, AnomalyMissingSeckillOrderRecord, row.OrderID)
			}

			if stock.RollbackCount > 0 && stock.DeductCount == 0 {
				r.markAnomaly(&sum, AnomalyStockRollbackWithoutDeduct, row.OrderID)
			}
			if isDBSuccess(row.Status) && stock.RollbackCount > 0 && stock.DeductCount > 0 {
				r.markAnomaly(&sum, AnomalyStockRollbackButOrderSuccess, row.OrderID)
			}
			if isDBFailed(row.Status) && stock.DeductCount > 0 && stock.RollbackCount == 0 {
				r.markAnomaly(&sum, AnomalyStockMissingRollbackOnFailed, row.OrderID)
			}

			if isDBSuccess(row.Status) && redisStatus != "success" {
				r.markAnomaly(&sum, AnomalyRedisNotSuccessOnDBSuccess, row.OrderID)
				r.doRepair(
					ctx,
					&sum,
					func(ctx context.Context) error {
						return r.client.UpdateOrderStatus(ctx, row.OrderID, "success", true)
					},
				)
			}

			if isDBFailed(row.Status) && (redisStatus == "pending" || redisStatus == "success") {
				r.markAnomaly(&sum, AnomalyRedisNotFailedOnDBFailed, row.OrderID)
				quantity := row.SeckillQuantity
				if quantity <= 0 {
					quantity = row.Quantity
				}
				if row.SeckillProductID <= 0 || row.UserID <= 0 || quantity <= 0 {
					r.logf(
						"level=warn msg=\"skip failed compensation due to incomplete fields\" order_id=%s seckill_product_id=%d user_id=%d quantity=%d",
						row.OrderID, row.SeckillProductID, row.UserID, quantity,
					)
					continue
				}

				r.doRepair(
					ctx,
					&sum,
					func(ctx context.Context) error {
						_, err := r.client.CompensateFailedOrder(
							ctx,
							row.OrderID,
							row.SeckillProductID,
							row.UserID,
							quantity,
							reconcileReasonDBFailed,
						)
						return err
					},
				)
			}
		}

		offset += len(rows)
	}

	return sum, nil
}

func (r *Runner) doRepair(ctx context.Context, sum *Summary, fn func(context.Context) error) {
	if r.cfg.DryRun {
		return
	}
	if sum.RepairAttempted >= r.cfg.MaxRepair {
		sum.RepairSkippedLimit++
		return
	}

	sum.RepairAttempted++
	if err := fn(ctx); err != nil {
		sum.RepairFailed++
		r.logf("level=error msg=\"repair failed\" err=%v", err)
		return
	}
	sum.RepairSucceeded++
}

func (r *Runner) markAnomaly(sum *Summary, anomalyType, orderID string) {
	sum.AnomalyCount[anomalyType]++
	r.logf("level=warn msg=\"anomaly found\" order_id=%s type=%s", orderID, anomalyType)
}

func isDBSuccess(status int32) bool {
	return status == OrderStatusPaid || status == OrderStatusCompleted
}

func isDBFailed(status int32) bool {
	return status == OrderStatusCancelled || status == OrderStatusRefunded
}
