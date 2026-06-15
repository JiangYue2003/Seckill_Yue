package server

import (
	commonpb "seckill-mall/common/common"
	orderpb "seckill-mall/common/order"
	"seckill-mall/order-service/internal/model/entity"
)

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

func mapPaymentStatus(status int32) commonpb.PaymentStatus {
	switch status {
	case entity.PaymentStatusRequested:
		return commonpb.PaymentStatus_PAYMENT_STATUS_REQUESTED
	case entity.PaymentStatusSuccess:
		return commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS
	case entity.PaymentStatusFailed:
		return commonpb.PaymentStatus_PAYMENT_STATUS_FAILED
	case entity.PaymentStatusClosed:
		return commonpb.PaymentStatus_PAYMENT_STATUS_CLOSED
	case entity.PaymentStatusRefunded:
		return commonpb.PaymentStatus_PAYMENT_STATUS_REFUNDED
	default:
		return commonpb.PaymentStatus_PAYMENT_STATUS_INIT
	}
}
