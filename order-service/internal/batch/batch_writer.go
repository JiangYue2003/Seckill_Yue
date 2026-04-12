package batch

import (
	"context"
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

	orderBuffer   []*entity.Order
	seckillBuffer []*entity.SeckillOrder

	maxBatchSize int           // 批量大小（默认 100）
	flushTimeout time.Duration // 超时刷盘（默认 500ms）

	mu    sync.Mutex
	timer *time.Timer
	done  chan struct{}
	wg    sync.WaitGroup
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
		orderBuffer:       make([]*entity.Order, 0, maxBatchSize),
		seckillBuffer:     make([]*entity.SeckillOrder, 0, maxBatchSize),
		maxBatchSize:      maxBatchSize,
		flushTimeout:      time.Duration(flushTimeoutMs) * time.Millisecond,
		done:              make(chan struct{}),
	}

	logx.Infof("BatchWriter initialized: maxBatchSize=%d, flushTimeout=%dms",
		maxBatchSize, flushTimeoutMs)

	return w
}

// AddOrder 添加订单到缓冲区
func (w *BatchWriter) AddOrder(order *entity.Order, seckillOrder *entity.SeckillOrder) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 检查是否已关闭
	select {
	case <-w.done:
		logx.Errorf("BatchWriter already closed, dropping order: %s", order.OrderId)
		return
	default:
	}

	// 添加到缓冲区
	w.orderBuffer = append(w.orderBuffer, order)
	if seckillOrder != nil {
		w.seckillBuffer = append(w.seckillBuffer, seckillOrder)
	}

	// 满了立即刷盘
	if len(w.orderBuffer) >= w.maxBatchSize {
		w.flushLocked()
		return
	}

	// 首次添加时启动超时定时器
	if w.timer == nil && len(w.orderBuffer) > 0 {
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
	if len(w.orderBuffer) == 0 {
		return
	}

	// 停止定时器
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}

	// 复制缓冲区（避免持锁时间过长）
	orders := make([]*entity.Order, len(w.orderBuffer))
	copy(orders, w.orderBuffer)
	seckillOrders := make([]*entity.SeckillOrder, len(w.seckillBuffer))
	copy(seckillOrders, w.seckillBuffer)

	// 清空缓冲区
	w.orderBuffer = w.orderBuffer[:0]
	w.seckillBuffer = w.seckillBuffer[:0]

	// 异步批量写入（不阻塞添加操作）
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.batchInsert(orders, seckillOrders)
	}()
}

// batchInsert 批量写入 MySQL
func (w *BatchWriter) batchInsert(orders []*entity.Order, seckillOrders []*entity.SeckillOrder) {
	ctx := context.Background()
	start := time.Now()

	// 1. 批量写入 orders 表
	affected, err := w.orderModel.BatchInsert(ctx, orders)
	if err != nil {
		logx.Errorf("Batch insert orders failed: count=%d, err=%v", len(orders), err)
		// 失败时回退到单条写入（兜底）
		w.fallbackInsertOrders(ctx, orders)
	} else {
		logx.Infof("Batch insert orders success: total=%d, inserted=%d, duration=%dms",
			len(orders), affected, time.Since(start).Milliseconds())
	}

	// 2. 批量写入 seckill_orders 表（异步，失败不影响主流程）
	if len(seckillOrders) > 0 {
		if err := w.seckillOrderModel.BatchInsert(ctx, seckillOrders); err != nil {
			logx.Errorf("Batch insert seckill_orders failed (non-critical): count=%d, err=%v",
				len(seckillOrders), err)
		}
	}
}

// fallbackInsertOrders 单条写入兜底
func (w *BatchWriter) fallbackInsertOrders(ctx context.Context, orders []*entity.Order) {
	logx.Infof("Fallback to single insert: count=%d", len(orders))
	for _, order := range orders {
		if err := w.orderModel.Insert(ctx, order); err != nil {
			logx.Errorf("Fallback insert failed: orderId=%s, err=%v", order.OrderId, err)
		}
	}
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
