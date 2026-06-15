package service

import (
	"context"
	"errors"
	"testing"

	seckillpb "seckill-mall/common/seckill"
	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/mq"
)

type fakeSeckillOrderTxManager struct {
	called int
	last   *model.PersistSeckillOrderInput
	result *model.PersistSeckillOrderResult
	err    error
}

func (f *fakeSeckillOrderTxManager) PersistSeckillOrder(ctx context.Context, in *model.PersistSeckillOrderInput) (*model.PersistSeckillOrderResult, error) {
	f.called++
	f.last = in
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &model.PersistSeckillOrderResult{}, nil
}

type fakeSeckillServiceRPC struct {
	updateCalls int
	lastOrderID string
	lastStatus  string
	lastRecover bool
}

func (f *fakeSeckillServiceRPC) UpdateOrderStatus(ctx context.Context, orderId, status string, allowRecover bool) error {
	f.updateCalls++
	f.lastOrderID = orderId
	f.lastStatus = status
	f.lastRecover = allowRecover
	return nil
}

func (f *fakeSeckillServiceRPC) CompensateFailedOrder(ctx context.Context, orderId string, seckillProductId int64, userId int64, quantity int64, reason string) (*seckillpb.CompensateFailedOrderResponse, error) {
	return &seckillpb.CompensateFailedOrderResponse{
		Success: true,
		Result:  "compensated",
	}, nil
}

func TestProcessSeckillOrderUsesTransactionalPersistence(t *testing.T) {
	txManager := &fakeSeckillOrderTxManager{}
	seckillRPC := &fakeSeckillServiceRPC{}
	svc := &OrderService{
		seckillSvcRPC:         seckillRPC,
		seckillOrderTxManager: txManager,
	}

	msg := &mq.SeckillOrderMessage{
		MessageId:        "msg-1",
		OrderId:          "order-1",
		UserId:           1001,
		SeckillProductId: 2001,
		ProductId:        3001,
		Quantity:         1,
		Amount:           999,
		SeckillPrice:     999,
	}

	if err := svc.ProcessSeckillOrder(msg); err != nil {
		t.Fatalf("ProcessSeckillOrder() error = %v", err)
	}

	if txManager.called != 1 {
		t.Fatalf("expected transactional persistence to be called once, got %d", txManager.called)
	}
	if txManager.last == nil {
		t.Fatal("expected persistence input to be captured")
	}
	if txManager.last.MessageID != "msg-1" {
		t.Fatalf("expected message id msg-1, got %s", txManager.last.MessageID)
	}
	if txManager.last.ConsumerName != seckillOrderConsumerName {
		t.Fatalf("expected consumer name %s, got %s", seckillOrderConsumerName, txManager.last.ConsumerName)
	}
	if seckillRPC.updateCalls != 0 {
		t.Fatalf("expected no seckill hot status update before payment, got %d", seckillRPC.updateCalls)
	}
}

func TestProcessSeckillOrderDuplicateDoesNotMarkSuccessBeforePayment(t *testing.T) {
	txManager := &fakeSeckillOrderTxManager{
		result: &model.PersistSeckillOrderResult{AlreadyProcessed: true},
	}
	seckillRPC := &fakeSeckillServiceRPC{}
	svc := &OrderService{
		seckillSvcRPC:         seckillRPC,
		seckillOrderTxManager: txManager,
	}

	msg := &mq.SeckillOrderMessage{
		MessageId: "msg-2",
		OrderId:   "order-2",
	}

	if err := svc.ProcessSeckillOrder(msg); err != nil {
		t.Fatalf("ProcessSeckillOrder() error = %v", err)
	}

	if seckillRPC.updateCalls != 0 {
		t.Fatalf("expected duplicate message not to mark success before payment, got %d updates", seckillRPC.updateCalls)
	}
}

func TestProcessSeckillOrderReturnsErrorWhenTransactionalPersistenceFails(t *testing.T) {
	txManager := &fakeSeckillOrderTxManager{err: errors.New("persist failed")}
	seckillRPC := &fakeSeckillServiceRPC{}
	svc := &OrderService{
		seckillSvcRPC:         seckillRPC,
		seckillOrderTxManager: txManager,
	}

	if err := svc.ProcessSeckillOrder(&mq.SeckillOrderMessage{MessageId: "msg-3", OrderId: "order-3"}); err == nil {
		t.Fatal("expected error when transactional persistence fails")
	}
	if seckillRPC.updateCalls != 0 {
		t.Fatalf("expected no success callback on persistence error, got %d", seckillRPC.updateCalls)
	}
}
