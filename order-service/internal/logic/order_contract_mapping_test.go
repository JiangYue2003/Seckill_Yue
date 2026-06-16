package logic

import (
	"testing"

	commonpb "seckill-mall/common/common"
	orderpb "seckill-mall/common/order"
	"seckill-mall/order-service/internal/model/entity"
)

func TestMapOrderLifecycleStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int32
		want   commonpb.OrderLifecycleStatus
	}{
		{name: "reserved", status: entity.OrderStatusReserved, want: commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_RESERVED},
		{name: "order_created", status: entity.OrderStatusOrderCreated, want: commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_ORDER_CREATED},
		{name: "paid", status: entity.OrderStatusPaid, want: commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_PAID},
		{name: "cancelled", status: entity.OrderStatusCancelled, want: commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_CANCELLED},
		{name: "refunded", status: entity.OrderStatusRefunded, want: commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_REFUNDED},
		{name: "completed", status: entity.OrderStatusCompleted, want: commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED},
		{name: "unknown", status: 99, want: commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_INIT},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapOrderLifecycleStatus(tt.status); got != tt.want {
				t.Fatalf("mapOrderLifecycleStatus(%d) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestBuildCompatiblePaymentRequestID(t *testing.T) {
	req := &orderpb.PayOrderRequest{OrderId: "o1"}
	if got := buildCompatiblePaymentID(req); got == "" {
		t.Fatal("expected generated compatible payment id")
	}

	explicit := &orderpb.PayOrderRequest{OrderId: "o2", PaymentId: "pay-1"}
	if got := buildCompatiblePaymentID(explicit); got != "compat-o2" {
		t.Fatalf("buildCompatiblePaymentID() = %s, want compat-o2", got)
	}

	withRequestID := &orderpb.PayOrderRequest{OrderId: "o3", PaymentId: "pay-legacy", RequestId: "req-3"}
	if got := buildCompatiblePaymentID(withRequestID); got != "req-3" {
		t.Fatalf("buildCompatiblePaymentID() = %s, want req-3", got)
	}
}

func TestMapPaymentStatusUsesPayStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int32
		want   commonpb.PaymentStatus
	}{
		{name: "init", status: entity.OrderPayStatusInit, want: commonpb.PaymentStatus_PAYMENT_STATUS_INIT},
		{name: "requested", status: entity.OrderPayStatusRequested, want: commonpb.PaymentStatus_PAYMENT_STATUS_REQUESTED},
		{name: "success", status: entity.OrderPayStatusSuccess, want: commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS},
		{name: "failed", status: entity.OrderPayStatusFailed, want: commonpb.PaymentStatus_PAYMENT_STATUS_FAILED},
		{name: "closed", status: entity.OrderPayStatusClosed, want: commonpb.PaymentStatus_PAYMENT_STATUS_CLOSED},
		{name: "refunded", status: entity.OrderPayStatusRefunded, want: commonpb.PaymentStatus_PAYMENT_STATUS_REFUNDED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapPaymentStatus(tt.status); got != tt.want {
				t.Fatalf("mapPaymentStatus(%d) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
