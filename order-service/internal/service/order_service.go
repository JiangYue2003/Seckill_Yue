package service

import (
	"context"
	"errors"
	"time"

	"seckill-mall/order-service/internal/batch"
	"seckill-mall/order-service/internal/metrics"
	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/model/entity"
	"seckill-mall/order-service/internal/mq"
	"seckill-mall/order-service/internal/rpc"

	"github.com/zeromicro/go-zero/core/logx"
)

// OrderService 订单服务
type OrderService struct {
	orderModel        model.OrderModel
	seckillOrderModel model.SeckillOrderModel
	productSvcRPC     *rpc.ProductServiceClient
	seckillSvcRPC     *rpc.SeckillServiceClient
	batchWriter       *batch.BatchWriter
}

func NewOrderService(orderModel model.OrderModel, seckillOrderModel model.SeckillOrderModel, batchWriter *batch.BatchWriter) *OrderService {
	return &OrderService{
		orderModel:        orderModel,
		seckillOrderModel: seckillOrderModel,
		batchWriter:       batchWriter,
	}
}

// SetProductServiceRPC 设置商品服务RPC客户端
func (s *OrderService) SetProductServiceRPC(svc *rpc.ProductServiceClient) {
	s.productSvcRPC = svc
}

// SetSeckillServiceRPC 设置秒杀服务RPC客户端
func (s *OrderService) SetSeckillServiceRPC(svc *rpc.SeckillServiceClient) {
	s.seckillSvcRPC = svc
}

// ProcessSeckillOrder 处理秒杀订单
// 职责边界：
// 1. 调用 Product-Service 扣减物理库存
// 2. 将订单加入批量写入缓冲区（幂等检查在 BatchWriter 内部批量执行）
func (s *OrderService) ProcessSeckillOrder(msg *mq.SeckillOrderMessage) error {
	ctx := context.Background()
	logger := logx.WithContext(ctx)
	start := time.Now()
	resultLabel := "failed"
	defer func() {
		metrics.OrderSeckillProcessTotal.WithLabelValues(resultLabel).Inc()
		metrics.OrderSeckillProcessDurationSeconds.WithLabelValues(resultLabel).Observe(time.Since(start).Seconds())
	}()

	// ========== 1. 调用 Product-Service 扣减物理库存（同步，必须成功）==========
	if s.productSvcRPC != nil {
		if err := s.productSvcRPC.DeductStock(ctx, msg.ProductId, msg.Quantity, msg.OrderId); err != nil {
			logger.Errorf("扣减物理库存失败: orderId=%s, err=%v", msg.OrderId, err)
			resultLabel = "deduct_stock_error"
			return err
		}
		logger.Debugf("扣减物理库存成功: orderId=%s, productId=%d, quantity=%d",
			msg.OrderId, msg.ProductId, msg.Quantity)
	}

	// ========== 2. 构造订单对象 ==========
	now := time.Now().Unix()
	order := &entity.Order{
		OrderId:      msg.OrderId,
		UserId:       msg.UserId,
		ProductId:    msg.ProductId,
		ProductName:  "",
		Quantity:     int(msg.Quantity),
		Amount:       msg.Amount,
		SeckillPrice: msg.SeckillPrice,
		OrderType:    entity.OrderTypeSeckill,
		Status:       entity.OrderStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	seckillRecord := &entity.SeckillOrder{
		UserId:           msg.UserId,
		SeckillProductId: msg.SeckillProductId,
		OrderId:          msg.OrderId,
		Quantity:         int(msg.Quantity),
		CreatedAt:        now,
	}

	// ========== 3. 加入批量写入缓冲区（幂等检查在 BatchWriter 内部）==========
	if s.batchWriter != nil {
		s.batchWriter.AddOrder(order, seckillRecord)
	} else {
		// 降级：BatchWriter 未初始化时，回退到同步写入（需要单独检查幂等）
		exists, err := s.orderModel.CheckIdempotency(ctx, msg.OrderId)
		if err != nil {
			logger.Errorf("检查幂等性失败: orderId=%s, err=%v", msg.OrderId, err)
			resultLabel = "idempotency_error"
			return err
		}
		if exists {
			logger.Debugf("订单已存在，跳过处理: orderId=%s", msg.OrderId)
			resultLabel = "idempotent_skip"
			return nil
		}

		if err := s.orderModel.Insert(ctx, order); err != nil {
			logger.Errorf("创建订单失败: orderId=%s, err=%v", msg.OrderId, err)
			resultLabel = "create_order_error"
			return err
		}
		if err := s.seckillOrderModel.Insert(ctx, seckillRecord); err != nil {
			logger.Errorf("写入秒杀购买记录失败: orderId=%s, err=%v", msg.OrderId, err)
		}
	}

	// ========== 4. 回写 Redis 订单状态为 success（降级：失败只记录日志）==========
	if s.seckillSvcRPC != nil {
		if rpcErr := s.seckillSvcRPC.UpdateOrderStatus(ctx, msg.OrderId, "success"); rpcErr != nil {
			logger.Errorf("回写 Redis 订单状态失败（不影响主流程）: orderId=%s, err=%v", msg.OrderId, rpcErr)
		} else {
			logger.Debugf("Redis 订单状态已更新为 success: orderId=%s", msg.OrderId)
		}
	}

	logger.Debugf("秒杀订单处理成功（已加入批量写入队列）: orderId=%s, userId=%d", msg.OrderId, msg.UserId)
	resultLabel = "success"
	return nil
}

// ProcessOrderTimeout 延迟队列超时兜底处理
// 秒杀成功后5分钟，检查订单是否仍处于 pending 状态
// 若 pending（MQ消费全部失败），标记订单失败并回滚MySQL库存
func (s *OrderService) ProcessOrderTimeout(msg *mq.SeckillOrderMessage) error {
	ctx := context.Background()
	logger := logx.WithContext(ctx)
	timeoutResult := "unknown"
	defer func() {
		metrics.OrderSeckillTimeoutTotal.WithLabelValues(timeoutResult).Inc()
	}()

	logger.Debugf("超时兜底检查触发: orderId=%s, userId=%d", msg.OrderId, msg.UserId)

	// 查询 MySQL 订单状态（权威来源）
	_, err := s.orderModel.FindOneByOrderId(ctx, msg.OrderId)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		logger.Errorf("查询订单状态失败: orderId=%s, err=%v", msg.OrderId, err)
		timeoutResult = "query_error"
		return err // 返回 error → Nack → DLX（可能是临时故障，走死信队列人工处理）
	}

	// 订单已在 MySQL 中创建 → ProcessSeckillOrder 曾经成功执行到建单步骤，无需回滚
	if err == nil {
		logger.Debugf("订单已创建，超时检查跳过: orderId=%s", msg.OrderId)
		timeoutResult = "skip_existing"
		return nil
	}

	// 订单在 MySQL 中不存在 → ProcessSeckillOrder 全部重试失败，执行兜底回滚
	logger.Errorf("订单超时未完成，执行兜底回滚: orderId=%s (not found in DB)", msg.OrderId)

	// 1. 更新 Redis 订单状态为 failed
	if s.seckillSvcRPC != nil {
		if rpcErr := s.seckillSvcRPC.UpdateOrderStatus(ctx, msg.OrderId, "failed"); rpcErr != nil {
			logger.Errorf("更新Redis订单状态失败: orderId=%s, err=%v", msg.OrderId, rpcErr)
			timeoutResult = "redis_update_failed"
			// 非致命，继续回滚库存
		}
	}

	// 2. 回滚 MySQL 物理库存（幂等，基于 orderId 防重）
	if s.productSvcRPC != nil {
		if rpcErr := s.productSvcRPC.RollbackStock(ctx, msg.ProductId, msg.Quantity, msg.OrderId); rpcErr != nil {
			logger.Errorf("回滚物理库存失败: orderId=%s, productId=%d, err=%v",
				msg.OrderId, msg.ProductId, rpcErr)
			timeoutResult = "rollback_failed"
			// 非致命，记录日志供人工处理
		}
	}

	logger.Debugf("超时兜底处理完成: orderId=%s", msg.OrderId)
	if timeoutResult == "unknown" {
		timeoutResult = "compensated_ok"
	}
	return nil // 总是 Ack，避免无限重试（已记录日志，人工处理残留问题）
}

// RollbackSeckillOrder 回滚秒杀订单（取消时调用）
func (s *OrderService) RollbackSeckillOrder(ctx context.Context, orderId string, productId, quantity int64) error {
	logger := logx.WithContext(ctx)

	// 1. 查询订单
	order, err := s.orderModel.FindOneByOrderId(ctx, orderId)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return errors.New("订单不存在")
		}
		return err
	}

	// 2. 只处理秒杀订单的库存回滚
	if order.OrderType != entity.OrderTypeSeckill {
		return nil
	}

	// 3. 回滚物理库存
	if s.productSvcRPC != nil {
		if err := s.productSvcRPC.RollbackStock(ctx, productId, quantity, orderId); err != nil {
			logger.Errorf("回滚物理库存失败: orderId=%s, err=%v", orderId, err)
			return err
		}
		logger.Debugf("回滚物理库存成功: orderId=%s, productId=%d, quantity=%d",
			orderId, productId, quantity)
	}

	return nil
}

// CreateNormalOrder 创建普通订单（仅扣减库存，不生成订单号）
func (s *OrderService) CreateNormalOrder(ctx context.Context, userId, productId, quantity int64, orderId string) error {
	logger := logx.WithContext(ctx)

	if userId <= 0 || productId <= 0 || quantity <= 0 {
		return errors.New("参数无效")
	}

	// 扣减物理库存
	if s.productSvcRPC != nil {
		if err := s.productSvcRPC.DeductStock(ctx, productId, quantity, orderId); err != nil {
			logger.Errorf("扣减物理库存失败: orderId=%s, productId=%d, err=%v", orderId, productId, err)
			return errors.New("库存扣减失败，请稍后重试")
		}
	}

	logger.Debugf("普通订单库存扣减成功: orderId=%s, productId=%d, quantity=%d", orderId, productId, quantity)
	return nil
}
