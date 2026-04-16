package reconciletest

import (
	"context"
	"testing"

	"github.com/seckill-mall/reconcile-tool/reconcile"
)

func TestRunDryRunDoesNotInvokeRepairs(t *testing.T) {
	repo := &fakeRepo{
		batches: [][]reconcile.OrderRow{
			{
				{
					OrderID:          "O-1",
					UserID:           1001,
					Quantity:         1,
					Status:           reconcile.OrderStatusPaid,
					SeckillProductID: 9001,
					SeckillQuantity:  1,
				},
			},
		},
	}
	store := &fakeStore{
		statusMap: map[string]string{"O-1": "pending"},
	}
	client := &fakeClient{}

	runner := mustNewRunner(t, reconcile.Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       100,
		DryRun:          true,
		MaxRepair:       10,
		LockKey:         "lk",
		LockTTLSeconds:  30,
	}, repo, store, client)

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !sum.Locked {
		t.Fatalf("expect lock acquired")
	}
	if got := sum.AnomalyCount[reconcile.AnomalyRedisNotSuccessOnDBSuccess]; got != 1 {
		t.Fatalf("unexpected anomaly count: %d", got)
	}
	if len(client.updateCalls) != 0 || len(client.compCalls) != 0 {
		t.Fatalf("dry-run should not invoke repairs, updates=%d comp=%d", len(client.updateCalls), len(client.compCalls))
	}
}

func TestRunRepairSuccessStatus(t *testing.T) {
	repo := &fakeRepo{
		batches: [][]reconcile.OrderRow{
			{
				{
					OrderID:          "O-2",
					UserID:           1002,
					Quantity:         1,
					Status:           reconcile.OrderStatusCompleted,
					SeckillProductID: 9002,
					SeckillQuantity:  1,
				},
			},
		},
	}
	store := &fakeStore{
		statusMap: map[string]string{"O-2": "failed"},
	}
	client := &fakeClient{}

	runner := mustNewRunner(t, reconcile.Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       100,
		DryRun:          false,
		MaxRepair:       10,
		LockKey:         "lk",
		LockTTLSeconds:  30,
	}, repo, store, client)

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(client.updateCalls) != 1 {
		t.Fatalf("expected one update call, got=%d", len(client.updateCalls))
	}
	if sum.RepairSucceeded != 1 || sum.RepairFailed != 0 {
		t.Fatalf("unexpected repair summary: %+v", sum)
	}
}

func TestRunRepairFailedStatusCompensate(t *testing.T) {
	repo := &fakeRepo{
		batches: [][]reconcile.OrderRow{
			{
				{
					OrderID:          "O-3",
					UserID:           1003,
					Quantity:         2,
					Status:           reconcile.OrderStatusCancelled,
					SeckillProductID: 9003,
					SeckillQuantity:  2,
				},
			},
		},
	}
	store := &fakeStore{
		statusMap: map[string]string{"O-3": "pending"},
	}
	client := &fakeClient{}

	runner := mustNewRunner(t, reconcile.Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       100,
		DryRun:          false,
		MaxRepair:       10,
		LockKey:         "lk",
		LockTTLSeconds:  30,
	}, repo, store, client)

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(client.compCalls) != 1 {
		t.Fatalf("expected one compensate call, got=%d", len(client.compCalls))
	}
	call := client.compCalls[0]
	if call.orderID != "O-3" || call.seckillProductID != 9003 || call.quantity != 2 {
		t.Fatalf("unexpected compensate args: %+v", call)
	}
	if sum.RepairSucceeded != 1 {
		t.Fatalf("unexpected summary: %+v", sum)
	}
}

func TestRunMissingSeckillOrderRecord(t *testing.T) {
	repo := &fakeRepo{
		batches: [][]reconcile.OrderRow{
			{
				{
					OrderID:          "O-4",
					UserID:           1004,
					Quantity:         1,
					Status:           reconcile.OrderStatusPending,
					SeckillProductID: 0,
				},
			},
		},
	}
	store := &fakeStore{
		statusMap: map[string]string{"O-4": "pending"},
	}
	client := &fakeClient{}

	runner := mustNewRunner(t, reconcile.Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       100,
		DryRun:          false,
		MaxRepair:       10,
		LockKey:         "lk",
		LockTTLSeconds:  30,
	}, repo, store, client)

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if got := sum.AnomalyCount[reconcile.AnomalyMissingSeckillOrderRecord]; got != 1 {
		t.Fatalf("expected missing record anomaly=1 got=%d", got)
	}
}

func TestRunMaxRepairLimit(t *testing.T) {
	repo := &fakeRepo{
		batches: [][]reconcile.OrderRow{
			{
				{
					OrderID:          "O-5-A",
					UserID:           1005,
					Quantity:         1,
					Status:           reconcile.OrderStatusPaid,
					SeckillProductID: 9005,
					SeckillQuantity:  1,
				},
				{
					OrderID:          "O-5-B",
					UserID:           1006,
					Quantity:         1,
					Status:           reconcile.OrderStatusPaid,
					SeckillProductID: 9006,
					SeckillQuantity:  1,
				},
			},
		},
	}
	store := &fakeStore{
		statusMap: map[string]string{
			"O-5-A": "pending",
			"O-5-B": "pending",
		},
	}
	client := &fakeClient{}

	runner := mustNewRunner(t, reconcile.Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       100,
		DryRun:          false,
		MaxRepair:       1,
		LockKey:         "lk",
		LockTTLSeconds:  30,
	}, repo, store, client)

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(client.updateCalls) != 1 {
		t.Fatalf("expected one update call under max-repair=1, got=%d", len(client.updateCalls))
	}
	if sum.RepairSkippedLimit != 1 {
		t.Fatalf("expected skipped-by-limit=1, got=%d", sum.RepairSkippedLimit)
	}
}

func TestRunLockNotAcquired(t *testing.T) {
	no := false
	repo := &fakeRepo{}
	store := &fakeStore{
		lockAcquired: &no,
	}
	client := &fakeClient{}

	runner := mustNewRunner(t, reconcile.Config{
		WindowStartUnix: 1,
		WindowEndUnix:   2,
		BatchSize:       100,
		DryRun:          false,
		MaxRepair:       10,
		LockKey:         "lk",
		LockTTLSeconds:  30,
	}, repo, store, client)

	sum, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if sum.Locked {
		t.Fatalf("expected locked=false when lock not acquired")
	}
	if repo.listCalls != 0 {
		t.Fatalf("should not query DB when lock not acquired")
	}
}

func mustNewRunner(
	t *testing.T,
	cfg reconcile.Config,
	repo reconcile.Repo,
	store reconcile.Store,
	client reconcile.SeckillClient,
) *reconcile.Runner {
	t.Helper()
	r, err := reconcile.NewRunner(cfg, repo, store, client, nil)
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}
	return r
}

type fakeRepo struct {
	batches   [][]reconcile.OrderRow
	listCalls int
	stockMap  map[string]reconcile.StockLogCount
}

func (f *fakeRepo) ListOrders(_ context.Context, _, _ int64, _, _ int) ([]reconcile.OrderRow, error) {
	if f.listCalls >= len(f.batches) {
		return nil, nil
	}
	out := f.batches[f.listCalls]
	f.listCalls++
	return out, nil
}

func (f *fakeRepo) GetStockLogCounts(_ context.Context, _ []string) (map[string]reconcile.StockLogCount, error) {
	if f.stockMap == nil {
		return map[string]reconcile.StockLogCount{}, nil
	}
	return f.stockMap, nil
}

type fakeStore struct {
	lockAcquired *bool
	statusMap    map[string]string
}

func (f *fakeStore) AcquireLock(_ context.Context, _, _ string, _ int64) (bool, error) {
	if f.lockAcquired != nil {
		return *f.lockAcquired, nil
	}
	return true, nil
}

func (f *fakeStore) ReleaseLock(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeStore) GetOrderStatuses(_ context.Context, _ []string) (map[string]string, error) {
	return f.statusMap, nil
}

type updateCall struct {
	orderID      string
	status       string
	allowRecover bool
}

type compensateCall struct {
	orderID          string
	seckillProductID int64
	userID           int64
	quantity         int64
	reason           string
}

type fakeClient struct {
	updateCalls []updateCall
	compCalls   []compensateCall
}

func (f *fakeClient) UpdateOrderStatus(_ context.Context, orderID, status string, allowRecover bool) error {
	f.updateCalls = append(f.updateCalls, updateCall{
		orderID:      orderID,
		status:       status,
		allowRecover: allowRecover,
	})
	return nil
}

func (f *fakeClient) CompensateFailedOrder(
	_ context.Context,
	orderID string,
	seckillProductID, userID, quantity int64,
	reason string,
) (string, error) {
	f.compCalls = append(f.compCalls, compensateCall{
		orderID:          orderID,
		seckillProductID: seckillProductID,
		userID:           userID,
		quantity:         quantity,
		reason:           reason,
	})
	return "compensated", nil
}
