package logic

import (
	commonpb "seckill-mall/common/common"
	"seckill-mall/seckill-service/internal/model/entity"
)

func mapReservationStatus(status int32) commonpb.ReservationStatus {
	switch status {
	case entity.ReservationStatusReserved:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED
	case entity.ReservationStatusOrderCreating:
		return commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATING
	case entity.ReservationStatusOrderCreated:
		return commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATED
	case entity.ReservationStatusPaying:
		return commonpb.ReservationStatus_RESERVATION_STATUS_PAYING
	case entity.ReservationStatusPaid:
		return commonpb.ReservationStatus_RESERVATION_STATUS_PAID
	case entity.ReservationStatusConsumed:
		return commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED
	case entity.ReservationStatusReleased:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RELEASED
	case entity.ReservationStatusExpired:
		return commonpb.ReservationStatus_RESERVATION_STATUS_EXPIRED
	case entity.ReservationStatusFailed:
		return commonpb.ReservationStatus_RESERVATION_STATUS_FAILED
	default:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED
	}
}

func mapReservationOrderLifecycle(status int32) commonpb.OrderLifecycleStatus {
	switch status {
	case entity.ReservationStatusReserved, entity.ReservationStatusOrderCreating:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_RESERVED
	case entity.ReservationStatusOrderCreated:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_ORDER_CREATED
	case entity.ReservationStatusPaying:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_PAYING
	case entity.ReservationStatusPaid:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_PAID
	case entity.ReservationStatusConsumed:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED
	case entity.ReservationStatusReleased:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_CANCELLED
	case entity.ReservationStatusExpired:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_EXPIRED
	case entity.ReservationStatusFailed:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_FAILED
	default:
		return commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_INIT
	}
}

func mapReservationPaymentStatus(status int32) commonpb.PaymentStatus {
	switch status {
	case entity.ReservationStatusPaying:
		return commonpb.PaymentStatus_PAYMENT_STATUS_REQUESTED
	case entity.ReservationStatusPaid, entity.ReservationStatusConsumed:
		return commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS
	case entity.ReservationStatusReleased, entity.ReservationStatusExpired:
		return commonpb.PaymentStatus_PAYMENT_STATUS_CLOSED
	case entity.ReservationStatusFailed:
		return commonpb.PaymentStatus_PAYMENT_STATUS_FAILED
	default:
		return commonpb.PaymentStatus_PAYMENT_STATUS_INIT
	}
}

func isReservationSuccess(status int32) bool {
	return status >= entity.ReservationStatusOrderCreated && status <= entity.ReservationStatusConsumed
}
