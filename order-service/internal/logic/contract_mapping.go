package logic

import (
	"fmt"

	commonpb "seckill-mall/common/common"
	orderpb "seckill-mall/common/order"
	"seckill-mall/order-service/internal/model/entity"
)

func mapOrderLifecycleStatus(status int32) commonpb.OrderLifecycleStatus {
	switch status {
	case entity.OrderStatusReserved:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_RESERVED
	case entity.OrderStatusOrderCreated:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_ORDER_CREATED
	case entity.OrderStatusPaying:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_PAYING
	case entity.OrderStatusPaid:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_PAID
	case entity.OrderStatusCancelled:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_CANCELLED
	case entity.OrderStatusExpired:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_EXPIRED
	case entity.OrderStatusFailed:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_FAILED
	case entity.OrderStatusRefunded:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_REFUNDED
	case entity.OrderStatusCompleted:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED
	default:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_INIT
	}
}

func mapPaymentStatus(status int32) commonpb.PaymentStatus {
	switch status {
	case entity.OrderPayStatusRequested:
		return commonpb.PaymentStatus_PAYMENT_STATUS_REQUESTED
	case entity.OrderPayStatusSuccess:
		return commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS
	case entity.OrderPayStatusInit:
		return commonpb.PaymentStatus_PAYMENT_STATUS_INIT
	case entity.OrderPayStatusClosed:
		return commonpb.PaymentStatus_PAYMENT_STATUS_CLOSED
	case entity.OrderPayStatusRefunded:
		return commonpb.PaymentStatus_PAYMENT_STATUS_REFUNDED
	case entity.OrderPayStatusFailed:
		return commonpb.PaymentStatus_PAYMENT_STATUS_FAILED
	default:
		return commonpb.PaymentStatus_PAYMENT_STATUS_INIT
	}
}

func buildOrderInfo(o *entity.Order) *orderpb.OrderInfo {
	return &orderpb.OrderInfo{
		OrderId:           o.OrderId,
		UserId:            o.UserId,
		ProductId:         o.ProductId,
		ProductName:       o.ProductName,
		Quantity:          int64(o.Quantity),
		Amount:            o.Amount,
		SeckillPrice:      o.SeckillPrice,
		OrderType:         o.OrderType,
		Status:            o.Status,
		PaymentId:         o.PaymentId,
		PaidAt:            o.PaidAt,
		ReservationId:     o.ReservationId,
		CreatedAt:         o.CreatedAt,
		UpdatedAt:         o.UpdatedAt,
		ExpiredAt:         o.ExpiredAt,
		ReservationStatus: mapReservationStatusFromOrder(o.Status),
		PaymentStatus:     mapPaymentStatus(o.PayStatus),
	}
}

func buildPaymentInfo(p *entity.Payment) *orderpb.PaymentInfo {
	if p == nil {
		return nil
	}
	return &orderpb.PaymentInfo{
		PaymentId:         p.PaymentId,
		OrderId:           p.OrderId,
		UserId:            p.UserId,
		Amount:            p.Amount,
		Channel:           p.Channel,
		Status:            mapPaymentStatus(p.Status),
		ThirdPartyTradeNo: p.ThirdPartyTradeNo,
		RequestId:         p.RequestId,
		PaidAt:            p.PaidAt,
		ClosedAt:          p.ClosedAt,
		CreatedAt:         p.CreatedAt,
		UpdatedAt:         p.UpdatedAt,
	}
}

func buildCompatiblePaymentID(in *orderpb.PayOrderRequest) string {
	if in.GetRequestId() != "" {
		return in.GetRequestId()
	}
	return fmt.Sprintf("compat-%s", in.GetOrderId())
}

func mapReservationStatusFromOrder(status int32) commonpb.ReservationStatus {
	switch status {
	case entity.OrderStatusReserved:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED
	case entity.OrderStatusOrderCreated:
		return commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATED
	case entity.OrderStatusPaying:
		return commonpb.ReservationStatus_RESERVATION_STATUS_PAYING
	case entity.OrderStatusPaid:
		return commonpb.ReservationStatus_RESERVATION_STATUS_PAID
	case entity.OrderStatusCompleted:
		return commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED
	case entity.OrderStatusCancelled:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RELEASED
	case entity.OrderStatusExpired:
		return commonpb.ReservationStatus_RESERVATION_STATUS_EXPIRED
	case entity.OrderStatusFailed:
		return commonpb.ReservationStatus_RESERVATION_STATUS_FAILED
	default:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED
	}
}
