package batch

import (
	"context"
	"strings"
	"sync"
	"time"

	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/model/entity"

	"github.com/zeromicro/go-zero/core/logx"
)

// BatchWriter 批量写入器
// 攒批后批量写入 MySQL，减少网络往返和事务开销
type BatchWriter struct {
	orderModel        model.OrderModel
	seckillOrderModel model.SeckillOrderModel

	buffer []*bufferedOrder

	maxBatchSize int           // 批量大小（默认 100）
	flushTimeout time.Duration // 超时刷盘（默认 500ms）

	mu    sync.Mutex
	timer *time.Timer
	done  chan struct{}
	wg    sync.WaitGroup
}

type bufferedOrder struct {
	order       *entity.Order
	seckill     *entity.SeckillOrder
	persistHook func(persisted bool)
}

// NewBatchWriter 创建批量写入器
func NewBatchWriter(
	orderModel model.OrderModel,
	seckillOrderModel model.SeckillOrderModel,
	maxBatchSize int,
	flushTimeoutMs int,
) *BatchWriter {
	if maxBatchSize <= 0 {
		maxBatchSize = 100
	}
	if flushTimeoutMs <= 0 {
		flushTimeoutMs = 500
	}

	w := &BatchWriter{
		orderModel:        orderModel,
		seckillOrderModel: seckillOrderModel,
		buffer:            make([]*bufferedOrder, 0, maxBatchSize),
		maxBatchSize:      maxBatchSize,
		flushTimeout:      time.Duration(flushTimeoutMs) * time.Millisecond,
		done:              make(chan struct{}),
	}

	logx.Infof("BatchWriter initialized: maxBatchSize=%d, flushTimeout=%dms",
		maxBatchSize, flushTimeoutMs)

	return w
}

// AddOrder 添加订单到缓冲区
// persistHook 会在订单确认已落库（或幂等命中已存在）后触发
func (w *BatchWriter) AddOrder(order *entity.Order, seckillOrder *entity.SeckillOrder, persistHook func(persisted bool)) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 检查是否已关闭
	select {
	case <-w.done:
		logx.Errorf("BatchWriter already closed, dropping order: %s", order.OrderId)
		triggerPersistHook(persistHook, false)
		return
	default:
	}

	// 添加到缓冲区
	w.buffer = append(w.buffer, &bufferedOrder{
		order:       order,
		seckill:     seckillOrder,
		persistHook: persistHook,
	})

	// 满了立即刷盘
	if len(w.buffer) >= w.maxBatchSize {
		w.flushLocked()
		return
	}

	// 首次添加时启动超时定时器
	if w.timer == nil && len(w.buffer) > 0 {
		w.timer = time.AfterFunc(w.flushTimeout, func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.flushLocked()
		})
	}
}

// Flush 手动刷盘（外部调用需加锁）
func (w *BatchWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
}

// flushLocked 刷盘（内部调用，已持有锁）
func (w *BatchWriter) flushLocked() {
	if len(w.buffer) == 0 {
		return
	}

	// 停止定时器
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}

	// 复制缓冲区（避免持锁时间过长）
	items := make([]*bufferedOrder, len(w.buffer))
	copy(items, w.buffer)

	// 清空缓冲区
	w.buffer = w.buffer[:0]

	// 异步批量写入（不阻塞添加操作）
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.batchInsert(items)
	}()
}

// batchInsert 批量写入 MySQL
func (w *BatchWriter) batchInsert(items []*bufferedOrder) {
	ctx := context.Background()
	start := time.Now()

	// 1. 批量幂等检查
	orderIds := make([]string, len(items))
	for i, item := range items {
		orderIds[i] = item.order.OrderId
	}

	existsMap, err := w.orderModel.BatchCheckIdempotency(ctx, orderIds)
	if err != nil {
		logx.Errorf("Batch check idempotency failed: %v", err)
		// 失败时回退到单条检查+写入
		w.fallbackInsertOrders(ctx, items)
		return
	}

	// 2. 过滤掉已存在的订单
	validItems := make([]*bufferedOrder, 0, len(items))
	duplicateCount := 0

	for _, item := range items {
		if existsMap[item.order.OrderId] {
			duplicateCount++
			triggerPersistHook(item.persistHook, true)
			continue // 跳过重复订单
		}
		validItems = append(validItems, item)
	}

	if duplicateCount > 0 {
		logx.Infof("Filtered duplicate orders: %d/%d", duplicateCount, len(items))
	}

	if len(validItems) == 0 {
		logx.Info("All orders are duplicates, skipping batch insert")
		return
	}

	validOrders := make([]*entity.Order, 0, len(validItems))
	validSeckillOrders := make([]*entity.SeckillOrder, 0, len(validItems))
	for _, item := range validItems {
		validOrders = append(validOrders, item.order)
		if item.seckill != nil {
			validSeckillOrders = append(validSeckillOrders, item.seckill)
		}
	}

	// 3. 批量写入有效订单
	affected, err := w.orderModel.BatchInsert(ctx, validOrders)
	if err != nil {
		logx.Errorf("Batch insert orders failed: count=%d, err=%v", len(validOrders), err)
		w.fallbackInsertOrders(ctx, validItems)
		return
	} else {
		logx.Infof("Batch insert orders success: total=%d, inserted=%d, duplicates=%d, duration=%dms",
			len(items), affected, duplicateCount, time.Since(start).Milliseconds())
		for _, item := range validItems {
			triggerPersistHook(item.persistHook, true)
		}
	}

	// 4. 批量写入 seckill_orders（异步，失败不影响主流程）
	if len(validSeckillOrders) > 0 {
		if err := w.seckillOrderModel.BatchInsert(ctx, validSeckillOrders); err != nil {
			logx.Errorf("Batch insert seckill_orders failed (non-critical): count=%d, err=%v",
				len(validSeckillOrders), err)
		}
	}
}

// fallbackInsertOrders 单条写入兜底
func (w *BatchWriter) fallbackInsertOrders(ctx context.Context, items []*bufferedOrder) {
	logx.Infof("Fallback to single insert: count=%d", len(items))
	for _, item := range items {
		if err := w.orderModel.Insert(ctx, item.order); err != nil {
			if isDuplicateInsertErr(err) {
				triggerPersistHook(item.persistHook, true)
				continue
			}
			logx.Errorf("Fallback insert failed: orderId=%s, err=%v", item.order.OrderId, err)
			triggerPersistHook(item.persistHook, false)
			continue
		}
		triggerPersistHook(item.persistHook, true)
	}
}

func isDuplicateInsertErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate")
}

func triggerPersistHook(hook func(persisted bool), persisted bool) {
	if hook == nil {
		return
	}
	go hook(persisted)
}

// Shutdown 优雅关闭（刷完所有缓冲区）
func (w *BatchWriter) Shutdown() {
	logx.Info("Flushing batch writer buffer before shutdown...")

	// 1. 标记关闭
	close(w.done)

	// 2. 刷盘
	w.Flush()

	// 3. 等待所有异步写入完成
	w.wg.Wait()

	logx.Info("Batch writer shutdown complete")
}
