package logic

import (
	"context"
	"errors"
	commonpb "seckill-mall/common/common"

	"seckill-mall/common/order"
	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/model/entity"
	"seckill-mall/order-service/internal/payment"
	"seckill-mall/order-service/internal/svc"

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
func (l *PayOrderLogic) PayOrder(in *order.PayOrderRequest) (*commonpb.BoolResponse, error) {
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

	// 检查订单状态
	if existingOrder.PayStatus == entity.OrderPayStatusSuccess &&
		(existingOrder.Status == entity.OrderStatusPaid || existingOrder.Status == entity.OrderStatusCompleted) {
		return &commonpb.BoolResponse{
			Success: true,
			Message: "支付成功",
		}, nil
	}
	if existingOrder.Status != entity.OrderStatusOrderCreated && existingOrder.Status != entity.OrderStatusPaying {
		return nil, errors.New("订单状态不正确，无法支付")
	}

	requestID := in.GetRequestId()
	if requestID == "" {
		requestID = buildCompatiblePaymentID(in)
	}
	created, err := l.svcCtx.PaymentService.CreatePayment(l.ctx, &payment.CreatePaymentInput{
		OrderID:      in.OrderId,
		Channel:      in.Channel,
		RequestID:    requestID,
		Operator:     "user",
		CallbackFrom: "mock_adapter",
	})
	if err != nil {
		if errors.Is(err, model.ErrOrderCannotPay) {
			return nil, errors.New("订单状态不正确，无法支付")
		}
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("订单不存在")
		}
		l.Logger.Errorf("创建支付请求失败: orderId=%s, err=%v", in.OrderId, err)
		return nil, errors.New("支付失败，请稍后重试")
	}

	l.Logger.Infof("订单支付链路完成: orderId=%s, paymentId=%s, channel=%s, requestId=%s", in.OrderId, created.PaymentId, in.Channel, requestID)

	return &commonpb.BoolResponse{
		Success: true,
		Message: "支付成功",
	}, nil
}
