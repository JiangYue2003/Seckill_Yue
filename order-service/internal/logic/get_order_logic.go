package logic

import (
	"context"
	"errors"

	"seckill-mall/common/order"
	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOrderLogic {
	return &GetOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetOrder 获取订单详情
func (l *GetOrderLogic) GetOrder(in *order.GetOrderRequest) (*order.OrderInfo, error) {
	// 参数校验
	if in.OrderId == "" {
		return nil, errors.New("订单号不能为空")
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

	return &order.OrderInfo{
		OrderId:      existingOrder.OrderId,
		UserId:       existingOrder.UserId,
		ProductId:    existingOrder.ProductId,
		ProductName:  existingOrder.ProductName,
		Quantity:     int64(existingOrder.Quantity),
		Amount:       existingOrder.Amount,
		SeckillPrice: existingOrder.SeckillPrice,
		OrderType:    existingOrder.OrderType,
		Status:       existingOrder.Status,
		PaymentId:    existingOrder.PaymentId,
		PaidAt:       existingOrder.PaidAt,
		CreatedAt:    existingOrder.CreatedAt,
		UpdatedAt:    existingOrder.UpdatedAt,
	}, nil
}
