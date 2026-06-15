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
	if got := buildCompatiblePaymentID(explicit); got != "pay-1" {
		t.Fatalf("buildCompatiblePaymentID() = %s, want pay-1", got)
	}
}
