package service

import (
	"context"
	"errors"
	"time"

	seckill "seckill-mall/common/seckill"
	"seckill-mall/order-service/internal/metrics"
	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/model/entity"
	"seckill-mall/order-service/internal/mq"
	"seckill-mall/order-service/internal/rpc"

	"github.com/zeromicro/go-zero/core/logx"
)

// OrderService 订单服务
type OrderService struct {
	orderModel            model.OrderModel
	seckillOrderModel     model.SeckillOrderModel
	productSvcRPC         *rpc.ProductServiceClient
	seckillSvcRPC         seckillStatusWriter
	seckillOrderTxManager model.SeckillOrderTxManager
}

const seckillOrderConsumerName = "order-service.seckill-order"

type seckillStatusWriter interface {
	UpdateOrderStatus(ctx context.Context, orderId, status string, allowRecover bool) error
	CompensateFailedOrder(ctx context.Context, orderId string, seckillProductId, userId, quantity int64, reason string, shardNo int32) (*seckill.CompensateFailedOrderResponse, error)
}

func NewOrderService(orderModel model.OrderModel, seckillOrderModel model.SeckillOrderModel, txManager model.SeckillOrderTxManager) *OrderService {
	return &OrderService{
		orderModel:            orderModel,
		seckillOrderModel:     seckillOrderModel,
		seckillOrderTxManager: txManager,
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
// 1. 消费 reservation.created 事件
// 2. 通过同事务持久化 orders / seckill_orders / seckill_reservations / processed_messages / order_status_logs / event_outbox
// 注意：秒杀场景下，Redis 只承担热点准入与热状态职责，不再作为最终购买事实来源。
func (s *OrderService) ProcessSeckillOrder(msg *mq.SeckillOrderMessage) error {
	ctx := context.Background()
	logger := logx.WithContext(ctx)
	start := time.Now()
	resultLabel := "failed"
	defer func() {
		metrics.OrderSeckillProcessTotal.WithLabelValues(resultLabel).Inc()
		metrics.OrderSeckillProcessDurationSeconds.WithLabelValues(resultLabel).Observe(time.Since(start).Seconds())
	}()

	// ========== 优化：去掉 Product-Service 扣库存调用 ==========
	// 秒杀场景下，Redis 库存已经在 seckill-service 中扣减
	// Product 表的 stock 字段是冗余的，秒杀结束后通过定时任务批量同步
	//
	// 原代码（已注释）：
	// if s.productSvcRPC != nil {
	//     if err := s.productSvcRPC.DeductStock(ctx, msg.ProductId, msg.Quantity, msg.OrderId); err != nil {
	//         logger.Errorf("扣减物理库存失败: orderId=%s, err=%v", msg.OrderId, err)
	//         resultLabel = "deduct_stock_error"
	//         return err
	//     }
	// }

	if s.seckillOrderTxManager == nil {
		resultLabel = "tx_manager_nil"
		return errors.New("seckill order tx manager is nil")
	}

	persistResult, err := s.seckillOrderTxManager.PersistSeckillOrder(ctx, &model.PersistSeckillOrderInput{
		MessageID:        msg.MessageId,
		ConsumerName:     seckillOrderConsumerName,
		OrderID:          msg.OrderId,
		UserID:           msg.UserId,
		SeckillProductID: msg.SeckillProductId,
		ProductID:        msg.ProductId,
		Quantity:         msg.Quantity,
		Amount:           msg.Amount,
		SeckillPrice:     msg.SeckillPrice,
		ShardNo:          msg.ShardNo,
	})
	if err != nil {
		logger.Errorf("事务化持久化秒杀订单失败: orderId=%s, messageId=%s, err=%v", msg.OrderId, msg.MessageId, err)
		resultLabel = "persist_order_error"
		return err
	}

	if persistResult != nil && persistResult.AlreadyProcessed {
		logger.Infof("秒杀订单消息已处理，跳过重复建单: orderId=%s, messageId=%s", msg.OrderId, msg.MessageId)
		metrics.ProcessedMessageDedupTotal.WithLabelValues(seckillOrderConsumerName).Inc()
	}

	logger.Debugf("秒杀订单处理成功（已完成事务落库）: orderId=%s, userId=%d", msg.OrderId, msg.UserId)
	resultLabel = "success"
	return nil
}

// ProcessOrderTimeout 延迟队列超时兜底处理
// 秒杀成功后5分钟，检查订单是否仍处于 pending 状态
// 若订单在 MySQL 不存在（主消费链路失败），触发 seckill-service 原子补偿：
// pending -> failed + 回补 Redis 库存 + 释放 userKey
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

	// 订单在 MySQL 中不存在 → ProcessSeckillOrder 全部重试失败，触发 Redis 原子补偿
	logger.Errorf("订单超时未完成，执行 failed 补偿: orderId=%s (not found in DB)", msg.OrderId)
	if s.seckillSvcRPC == nil {
		timeoutResult = "compensate_client_nil"
		return errors.New("seckill rpc client is nil")
	}

	compensateResp, rpcErr := s.seckillSvcRPC.CompensateFailedOrder(
		ctx,
		msg.OrderId,
		msg.SeckillProductId,
		msg.UserId,
		msg.Quantity,
		"timeout_not_found_in_db",
		msg.ShardNo,
	)
	if rpcErr != nil {
		logger.Errorf("failed compensation rpc error: orderId=%s, err=%v", msg.OrderId, rpcErr)
		timeoutResult = "compensate_rpc_error"
		return rpcErr
	}

	switch compensateResp.GetResult() {
	case "compensated":
		timeoutResult = "compensated_ok"
	case "idempotent_failed":
		timeoutResult = "compensated_idempotent"
	case "already_success":
		timeoutResult = "skip_already_success"
	case "order_not_found":
		timeoutResult = "skip_order_missing"
	default:
		timeoutResult = "compensate_unexpected_result"
	}
	logger.Debugf("超时补偿处理完成: orderId=%s, result=%s", msg.OrderId, compensateResp.GetResult())
	return nil
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
			if rpc.IsNoDeductRecordError(err) {
				logger.Infof("跳过秒杀库存回滚（无扣减记录）: orderId=%s, productId=%d", orderId, productId)
				return nil
			}
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
