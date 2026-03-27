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

type CancelOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCancelOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelOrderLogic {
	return &CancelOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CancelOrder 取消订单
func (l *CancelOrderLogic) CancelOrder(in *order.CancelOrderRequest) (*order.BoolResponse, error) {
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

	// 检查订单状态
	if existingOrder.Status != entity.OrderStatusPending {
		return nil, errors.New("只有待支付的订单才能取消")
	}

	// 检查用户权限
	if existingOrder.UserId != in.UserId {
		return nil, errors.New("无权取消此订单")
	}

	// 取消订单
	if err := l.svcCtx.OrderModel.Cancel(l.ctx, in.OrderId, in.UserId); err != nil {
		if errors.Is(err, model.ErrOrderCannotCancel) {
			return nil, errors.New("订单状态不正确，无法取消")
		}
		l.Logger.Errorf("取消订单失败: orderId=%s, err=%v", in.OrderId, err)
		return nil, errors.New("取消订单失败，请稍后重试")
	}

	// 回滚库存（仅秒杀订单）
	if existingOrder.OrderType == entity.OrderTypeSeckill {
		if err := l.svcCtx.OrderService.RollbackSeckillOrder(l.ctx, in.OrderId, existingOrder.ProductId, int64(existingOrder.Quantity)); err != nil {
			l.Logger.Errorf("回滚秒杀订单库存失败: orderId=%s, err=%v", in.OrderId, err)
			// 不阻塞，订单已取消，库存回滚失败可人工处理
		}
	} else {
		// 普通订单也回滚库存
		if l.svcCtx.ProductServiceRPC != nil {
			if err := l.svcCtx.ProductServiceRPC.RollbackStock(l.ctx, existingOrder.ProductId, int64(existingOrder.Quantity), in.OrderId); err != nil {
				l.Logger.Errorf("回滚普通订单库存失败: orderId=%s, err=%v", in.OrderId, err)
			}
		}
	}

	l.Logger.Infof("订单取消成功: orderId=%s, userId=%d", in.OrderId, in.UserId)

	return &order.BoolResponse{
		Success: true,
		Message: "订单取消成功",
	}, nil
}
