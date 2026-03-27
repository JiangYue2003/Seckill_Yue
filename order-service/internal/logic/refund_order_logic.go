package logic

import (
	"context"
	"errors"

	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/model/entity"
	"seckill-mall/order-service/internal/svc"
	"seckill-mall/order-service/order"

	"github.com/zeromicro/go-zero/core/logx"
)

type RefundOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRefundOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RefundOrderLogic {
	return &RefundOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RefundOrder 退款
func (l *RefundOrderLogic) RefundOrder(in *order.RefundOrderRequest) (*order.BoolResponse, error) {
	// 参数校验
	if in.OrderId == "" {
		return nil, errors.New("订单号不能为空")
	}
	if in.UserId <= 0 {
		return nil, errors.New("用户ID无效")
	}

	// 查询订单
	existingOrder, err := l.svcCtx.OrderModel.FindOneByOrderId(l.ctx, in.OrderId)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("订单不存在")
		}
		l.Logger.Errorf("查询订单失败: orderId=%s, err=%v", in.OrderId, err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 检查用户ID
	if existingOrder.UserId != in.UserId {
		return nil, errors.New("无权操作此订单")
	}

	// 检查订单状态：已支付或已完成才能退款
	if existingOrder.Status != entity.OrderStatusPaid && existingOrder.Status != entity.OrderStatusCompleted {
		return nil, errors.New("订单状态不正确，无法退款")
	}

	// 退款
	if err := l.svcCtx.OrderModel.Refund(l.ctx, in.OrderId); err != nil {
		if errors.Is(err, model.ErrOrderCannotRefund) {
			return nil, errors.New("订单状态不正确，无法退款")
		}
		l.Logger.Errorf("退款失败: orderId=%s, err=%v", in.OrderId, err)
		return nil, errors.New("退款失败，请稍后重试")
	}

	// 回滚库存
	if existingOrder.OrderType == entity.OrderTypeSeckill {
		if err := l.svcCtx.OrderService.RollbackSeckillOrder(l.ctx, in.OrderId, existingOrder.ProductId, int64(existingOrder.Quantity)); err != nil {
			l.Logger.Errorf("回滚秒杀订单库存失败: orderId=%s, err=%v", in.OrderId, err)
		}
	} else {
		if l.svcCtx.ProductServiceRPC != nil {
			if err := l.svcCtx.ProductServiceRPC.RollbackStock(l.ctx, existingOrder.ProductId, int64(existingOrder.Quantity), in.OrderId); err != nil {
				l.Logger.Errorf("回滚普通订单库存失败: orderId=%s, err=%v", in.OrderId, err)
			}
		}
	}

	l.Logger.Infof("退款成功: orderId=%s, reason=%s", in.OrderId, in.Reason)

	return &order.BoolResponse{
		Success: true,
		Message: "退款成功",
	}, nil
}
