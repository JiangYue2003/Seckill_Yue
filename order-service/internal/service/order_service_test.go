package service

import (
	"context"
	"errors"
	"testing"

	seckillpb "seckill-mall/common/seckill"
	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/model/entity"
	"seckill-mall/order-service/internal/mq"
)

type fakeSeckillOrderTxManager struct {
	called int
	last   *model.PersistSeckillOrderInput
	result *model.PersistSeckillOrderResult
	err    error
}

type fakeOrderModel struct {
	findOrderID string
	findResult  *entity.Order
	findErr     error
}

func (f *fakeOrderModel) FindOneByOrderId(ctx context.Context, orderId string) (*entity.Order, error) {
	f.findOrderID = orderId
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.findResult, nil
}

func (f *fakeOrderModel) FindByUserId(ctx context.Context, userId int64, status int32, page, pageSize int64) ([]*entity.Order, int64, error) {
	panic("not implemented")
}

func (f *fakeOrderModel) Insert(ctx context.Context, order *entity.Order) error {
	panic("not implemented")
}

func (f *fakeOrderModel) BatchInsert(ctx context.Context, orders []*entity.Order) (int64, error) {
	panic("not implemented")
}

func (f *fakeOrderModel) Update(ctx context.Context, order *entity.Order) error {
	panic("not implemented")
}

func (f *fakeOrderModel) UpdateStatus(ctx context.Context, orderId string, status int32) error {
	panic("not implemented")
}

func (f *fakeOrderModel) Cancel(ctx context.Context, orderId string, userId int64) error {
	panic("not implemented")
}

func (f *fakeOrderModel) Refund(ctx context.Context, orderId string) error {
	panic("not implemented")
}

func (f *fakeOrderModel) CheckIdempotency(ctx context.Context, orderId string) (bool, error) {
	panic("not implemented")
}

func (f *fakeOrderModel) BatchCheckIdempotency(ctx context.Context, orderIds []string) (map[string]bool, error) {
	panic("not implemented")
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
	compensateCalls int
	lastCompensateOrderID string
	lastCompensateProductID int64
	lastCompensateUserID int64
	lastCompensateQuantity int64
	lastCompensateReason string
	compensateResp *seckillpb.CompensateFailedOrderResponse
	compensateErr error
}

func (f *fakeSeckillServiceRPC) UpdateOrderStatus(ctx context.Context, orderId, status string, allowRecover bool) error {
	f.updateCalls++
	f.lastOrderID = orderId
	f.lastStatus = status
	f.lastRecover = allowRecover
	return nil
}

func (f *fakeSeckillServiceRPC) CompensateFailedOrder(ctx context.Context, orderId string, seckillProductId int64, userId int64, quantity int64, reason string) (*seckillpb.CompensateFailedOrderResponse, error) {
	f.compensateCalls++
	f.lastCompensateOrderID = orderId
	f.lastCompensateProductID = seckillProductId
	f.lastCompensateUserID = userId
	f.lastCompensateQuantity = quantity
	f.lastCompensateReason = reason
	if f.compensateErr != nil {
		return nil, f.compensateErr
	}
	if f.compensateResp != nil {
		return f.compensateResp, nil
	}
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

	if txManager.called != 1 {
		t.Fatalf("expected transactional persistence to be called once, got %d", txManager.called)
	}
	if txManager.last == nil {
		t.Fatal("expected persistence input to be captured for duplicate message")
	}
	if txManager.last.MessageID != "msg-2" {
		t.Fatalf("expected duplicate message id msg-2, got %s", txManager.last.MessageID)
	}
	if txManager.last.ConsumerName != seckillOrderConsumerName {
		t.Fatalf("expected consumer name %s, got %s", seckillOrderConsumerName, txManager.last.ConsumerName)
	}
	if seckillRPC.updateCalls != 0 {
		t.Fatalf("expected duplicate message not to mark success before payment, got %d updates", seckillRPC.updateCalls)
	}
	if seckillRPC.compensateCalls != 0 {
		t.Fatalf("expected duplicate message not to trigger compensation, got %d calls", seckillRPC.compensateCalls)
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

func TestProcessOrderTimeoutCompensatesWhenOrderNotFound(t *testing.T) {
	seckillRPC := &fakeSeckillServiceRPC{}
	svc := &OrderService{
		orderModel:     &fakeOrderModel{findErr: model.ErrNotFound},
		seckillSvcRPC:  seckillRPC,
	}

	msg := &mq.SeckillOrderMessage{
		OrderId:          "order-timeout-1",
		UserId:           1001,
		SeckillProductId: 2001,
		Quantity:         2,
	}

	if err := svc.ProcessOrderTimeout(msg); err != nil {
		t.Fatalf("ProcessOrderTimeout() error = %v", err)
	}
	if seckillRPC.compensateCalls != 1 {
		t.Fatalf("expected compensate called once, got %d", seckillRPC.compensateCalls)
	}
	if seckillRPC.lastCompensateOrderID != "order-timeout-1" ||
		seckillRPC.lastCompensateProductID != 2001 ||
		seckillRPC.lastCompensateUserID != 1001 ||
		seckillRPC.lastCompensateQuantity != 2 ||
		seckillRPC.lastCompensateReason != "timeout_not_found_in_db" {
		t.Fatalf("unexpected compensate args: order=%s product=%d user=%d quantity=%d reason=%s",
			seckillRPC.lastCompensateOrderID,
			seckillRPC.lastCompensateProductID,
			seckillRPC.lastCompensateUserID,
			seckillRPC.lastCompensateQuantity,
			seckillRPC.lastCompensateReason,
		)
	}
}

func TestProcessOrderTimeoutSkipsCompensationWhenOrderExists(t *testing.T) {
	seckillRPC := &fakeSeckillServiceRPC{}
	svc := &OrderService{
		orderModel: &fakeOrderModel{findResult: &entity.Order{
			OrderId: "order-timeout-2",
		}},
		seckillSvcRPC: seckillRPC,
	}

	msg := &mq.SeckillOrderMessage{
		OrderId:          "order-timeout-2",
		UserId:           1002,
		SeckillProductId: 2002,
		Quantity:         1,
	}

	if err := svc.ProcessOrderTimeout(msg); err != nil {
		t.Fatalf("ProcessOrderTimeout() error = %v", err)
	}
	if seckillRPC.compensateCalls != 0 {
		t.Fatalf("expected no compensation when order exists, got %d", seckillRPC.compensateCalls)
	}
}

func TestProcessOrderTimeoutReturnsErrorWhenCompensationFails(t *testing.T) {
	seckillRPC := &fakeSeckillServiceRPC{compensateErr: errors.New("rpc failed")}
	svc := &OrderService{
		orderModel:    &fakeOrderModel{findErr: model.ErrNotFound},
		seckillSvcRPC: seckillRPC,
	}

	msg := &mq.SeckillOrderMessage{
		OrderId:          "order-timeout-3",
		UserId:           1003,
		SeckillProductId: 2003,
		Quantity:         1,
	}

	if err := svc.ProcessOrderTimeout(msg); err == nil {
		t.Fatal("expected compensation rpc error")
	}
	if seckillRPC.compensateCalls != 1 {
		t.Fatalf("expected compensate called once, got %d", seckillRPC.compensateCalls)
	}
}
