package logic

import (
	"context"
	"errors"

	commonpb "seckill-mall/common/common"
	"seckill-mall/common/seckill"
	"seckill-mall/seckill-service/internal/model"
	"seckill-mall/seckill-service/internal/model/entity"
	"seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetSeckillResultLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSeckillResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSeckillResultLogic {
	return &GetSeckillResultLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetSeckillResult 查询秒杀结果
// 通过 orderId 从 Redis 查询完整订单信息
func (l *GetSeckillResultLogic) GetSeckillResult(in *seckill.SeckillResultRequest) (*seckill.SeckillResultResponse, error) {
	// 参数校验
	if in.OrderId == "" {
		return &seckill.SeckillResultResponse{
			Success:       false,
			Message:       "订单号不能为空",
			OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_FAILED,
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_FAILED,
		}, nil
	}

	// 从 Redis 获取完整订单信息
	orderInfo, err := l.svcCtx.Redis.GetOrderInfo(l.ctx, in.OrderId)
	if err != nil {
		l.Logger.Errorf("查询订单信息失败: orderId=%s, err=%v", in.OrderId, err)
		if l.svcCtx.ReservationLedger == nil {
			return &seckill.SeckillResultResponse{
				Success:       false,
				OrderId:       in.OrderId,
				Message:       "查询订单信息失败",
				PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_FAILED,
			}, nil
		}
	}

	// 订单不存在或已过期（TTL 过期后返回 nil）
	if orderInfo == nil {
		if l.svcCtx.ReservationLedger != nil {
			reservation, reservationErr := l.svcCtx.ReservationLedger.GetReservation(l.ctx, "", in.OrderId)
			if reservationErr == nil && reservation != nil {
				return &seckill.SeckillResultResponse{
					Success:           isReservationSuccess(reservation.Status),
					OrderId:           reservation.OrderId,
					ProductId:         reservation.ProductId,
					Quantity:          int64(reservation.Quantity),
					Amount:            reservation.Amount,
					Status:            reservationStatusText(reservation.Status),
					Message:           "订单事实已落库，Redis 热状态同步中",
					ReservationId:     reservation.ReservationId,
					ReservationStatus: mapReservationStatus(reservation.Status),
					OrderStatus:       mapReservationOrderLifecycle(reservation.Status),
					PaymentStatus:     mapReservationPaymentStatus(reservation.Status),
				}, nil
			}
			if reservationErr != nil && !errors.Is(reservationErr, model.ErrNotFound) {
				l.Logger.Errorf("查询 reservation 事实失败: orderId=%s, err=%v", in.OrderId, reservationErr)
			}
		}
		return &seckill.SeckillResultResponse{
			Success:       false,
			OrderId:       in.OrderId,
			Message:       "订单不存在或已过期",
			OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_EXPIRED,
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_CLOSED,
		}, nil
	}

	// 根据状态返回结果
	switch orderInfo.Status {
	case redis.OrderStatusSuccess:
		// 最小状态落点下，详情可能尚未补全到 Redis，保持 success 语义不变
		if orderInfo.ProductId <= 0 || orderInfo.Quantity <= 0 {
			return &seckill.SeckillResultResponse{
				Success:           true,
				OrderId:           in.OrderId,
				Status:            orderInfo.Status,
				Message:           "订单已成功，详情同步中，请稍后重试",
				ReservationId:     in.OrderId,
				ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_PAID,
				OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_PAID,
				PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS,
			}, nil
		}

		productName := orderInfo.ProductName
		if productName == "" {
			productName = "秒杀商品"
		}
		return &seckill.SeckillResultResponse{
			Success:           true,
			OrderId:           in.OrderId,
			ProductId:         orderInfo.ProductId,
			ProductName:       productName,
			Quantity:          orderInfo.Quantity,
			Amount:            orderInfo.Amount,
			Status:            orderInfo.Status,
			Message:           "订单处理成功",
			ReservationId:     in.OrderId,
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_PAID,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS,
		}, nil

	case redis.OrderStatusPending:
		return &seckill.SeckillResultResponse{
			Success:           false,
			OrderId:           in.OrderId,
			Status:            orderInfo.Status,
			Message:           "订单正在处理中，请稍后查询",
			ReservationId:     in.OrderId,
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_RESERVED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_INIT,
		}, nil

	case redis.OrderStatusFailed:
		return &seckill.SeckillResultResponse{
			Success:           false,
			OrderId:           in.OrderId,
			Status:            orderInfo.Status,
			Message:           "订单处理失败",
			ReservationId:     in.OrderId,
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_FAILED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_FAILED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_FAILED,
		}, nil

	default:
		return &seckill.SeckillResultResponse{
			Success:       false,
			OrderId:       in.OrderId,
			Message:       "未知的订单状态",
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_FAILED,
		}, nil
	}
}

func reservationStatusText(status int32) string {
	switch {
	case isReservationSuccess(status):
		return OrderStatusSuccess
	case status == entity.ReservationStatusFailed:
		return OrderStatusFailed
	default:
		return OrderStatusPending
	}
}
