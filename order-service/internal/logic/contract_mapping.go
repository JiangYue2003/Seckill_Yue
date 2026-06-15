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
	case entity.OrderStatusPaid:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_PAID
	case entity.OrderStatusCancelled:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_CANCELLED
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
	case entity.OrderStatusPaid, entity.OrderStatusCompleted:
		return commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS
	case entity.OrderStatusOrderCreated, entity.OrderStatusReserved:
		return commonpb.PaymentStatus_PAYMENT_STATUS_INIT
	case entity.OrderStatusCancelled:
		return commonpb.PaymentStatus_PAYMENT_STATUS_CLOSED
	case entity.OrderStatusRefunded:
		return commonpb.PaymentStatus_PAYMENT_STATUS_REFUNDED
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
		ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATED,
		PaymentStatus:     mapPaymentStatus(o.Status),
	}
}

func buildCompatiblePaymentID(in *orderpb.PayOrderRequest) string {
	if in.GetPaymentId() != "" {
		return in.GetPaymentId()
	}
	if in.GetRequestId() != "" {
		return fmt.Sprintf("compat-%s", in.GetRequestId())
	}
	return fmt.Sprintf("compat-%s", in.GetOrderId())
}
