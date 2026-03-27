package logic

import (
	"context"
	"errors"

	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/svc"
	"seckill-mall/order-service/order"

	"github.com/zeromicro/go-zero/core/logx"
)

type PayOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPayOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PayOrderLogic {
	return &PayOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// PayOrder 支付订单
func (l *PayOrderLogic) PayOrder(in *order.PayOrderRequest) (*order.BoolResponse, error) {
	// 参数校验
	if in.OrderId == "" {
		return nil, errors.New("订单号不能为空")
	}
	if in.PaymentId == "" {
		return nil, errors.New("支付流水号不能为空")
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
	if existingOrder.Status != 0 { // 非待支付状态不能支付
		return nil, errors.New("订单状态不正确，无法支付")
	}

	// 支付订单
	if err := l.svcCtx.OrderModel.Pay(l.ctx, in.OrderId, in.PaymentId); err != nil {
		if errors.Is(err, model.ErrOrderCannotPay) {
			return nil, errors.New("订单状态不正确，无法支付")
		}
		l.Logger.Errorf("支付订单失败: orderId=%s, err=%v", in.OrderId, err)
		return nil, errors.New("支付失败，请稍后重试")
	}

	l.Logger.Infof("订单支付成功: orderId=%s, paymentId=%s", in.OrderId, in.PaymentId)

	return &order.BoolResponse{
		Success: true,
		Message: "支付成功",
	}, nil
}
