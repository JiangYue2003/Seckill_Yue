package payment

import (
	"context"
	"errors"
	"testing"

	"seckill-mall/order-service/internal/model/entity"
)

type fakeLedger struct {
	createCalls       int
	createInput       *CreatePaymentInput
	createResult      *CreatePaymentResult
	createErr         error
	requestedCalls    int
	requestedInput    *MarkPaymentRequestedInput
	requestedResult   *entity.Payment
	requestedErr      error
	callbackCalls     int
	callbackInput     *HandlePaymentCallbackInput
	callbackResult    *HandlePaymentCallbackResult
	callbackErr       error
	getPaymentCalls   int
	getPaymentID      string
	getPaymentOrderID string
	getPaymentResult  *entity.Payment
	getPaymentErr     error
}

func (f *fakeLedger) CreatePayment(ctx context.Context, in *CreatePaymentInput) (*CreatePaymentResult, error) {
	f.createCalls++
	f.createInput = in
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeLedger) MarkPaymentRequested(ctx context.Context, in *MarkPaymentRequestedInput) (*entity.Payment, error) {
	f.requestedCalls++
	f.requestedInput = in
	if f.requestedErr != nil {
		return nil, f.requestedErr
	}
	return f.requestedResult, nil
}

func (f *fakeLedger) HandlePaymentCallback(ctx context.Context, in *HandlePaymentCallbackInput) (*HandlePaymentCallbackResult, error) {
	f.callbackCalls++
	f.callbackInput = in
	if f.callbackErr != nil {
		return nil, f.callbackErr
	}
	return f.callbackResult, nil
}

func (f *fakeLedger) GetPayment(ctx context.Context, paymentID, orderID string) (*entity.Payment, error) {
	f.getPaymentCalls++
	f.getPaymentID = paymentID
	f.getPaymentOrderID = orderID
	if f.getPaymentErr != nil {
		return nil, f.getPaymentErr
	}
	return f.getPaymentResult, nil
}

type fakeAdapter struct {
	requestCalls int
	requestInput *RequestPaymentInput
	requestResp  *RequestPaymentResult
	requestErr   error
}

func (f *fakeAdapter) RequestPayment(ctx context.Context, in *RequestPaymentInput) (*RequestPaymentResult, error) {
	f.requestCalls++
	f.requestInput = in
	if f.requestErr != nil {
		return nil, f.requestErr
	}
	return f.requestResp, nil
}

type fakeHotOrderWriter struct {
	updateCalls      int
	orderID          string
	status           string
	recover          bool
	advanceCalls     int
	advanceOrderID   string
	advanceStatus    int32
	advanceReason    string
	advanceOperator  string
	advancePaymentID string
	err              error
}

func (f *fakeHotOrderWriter) UpdateOrderStatus(ctx context.Context, orderID, status string, allowRecover bool) error {
	f.updateCalls++
	f.orderID = orderID
	f.status = status
	f.recover = allowRecover
	return f.err
}

func (f *fakeHotOrderWriter) AdvanceReservation(ctx context.Context, in *AdvanceReservationInput) error {
	f.advanceCalls++
	if in != nil {
		f.advanceOrderID = in.OrderID
		f.advanceStatus = in.TargetStatus
		f.advanceReason = in.Reason
		f.advanceOperator = in.Operator
		f.advancePaymentID = in.PaymentID
	}
	return f.err
}

func TestServiceCreatePaymentRunsFullMockPaymentFlow(t *testing.T) {
	ledger := &fakeLedger{
		createResult: &CreatePaymentResult{
			Payment: &entity.Payment{
				PaymentId: "pay-1",
				OrderId:   "order-1",
				UserId:    1001,
				Amount:    9900,
				Channel:   "mock_alipay",
				Status:    entity.PaymentStatusInit,
				RequestId: "req-1",
			},
		},
		requestedResult: &entity.Payment{
			PaymentId: "pay-1",
			OrderId:   "order-1",
			UserId:    1001,
			Amount:    9900,
			Channel:   "mock_alipay",
			Status:    entity.PaymentStatusRequested,
			RequestId: "req-1",
		},
		callbackResult: &HandlePaymentCallbackResult{
			Payment: &entity.Payment{
				PaymentId:         "pay-1",
				OrderId:           "order-1",
				UserId:            1001,
				Amount:            9900,
				Channel:           "mock_alipay",
				Status:            entity.PaymentStatusSuccess,
				RequestId:         "req-1",
				ThirdPartyTradeNo: "trade-1",
			},
		},
	}
	adapter := &fakeAdapter{
		requestResp: &RequestPaymentResult{
			ThirdPartyTradeNo: "trade-1",
			CallbackID:        "cbk-1",
			RawPayload:        `{"payment_id":"pay-1","trade_no":"trade-1"}`,
		},
	}
	hotWriter := &fakeHotOrderWriter{}
	svc := NewService(ledger, adapter, hotWriter)

	got, err := svc.CreatePayment(context.Background(), &CreatePaymentInput{
		OrderID:      "order-1",
		Channel:      "mock_alipay",
		RequestID:    "req-1",
		Operator:     "user",
		CallbackFrom: "mock-adapter",
	})
	if err != nil {
		t.Fatalf("CreatePayment() error = %v", err)
	}

	if got == nil || got.PaymentId != "pay-1" || got.Status != entity.PaymentStatusSuccess {
		t.Fatalf("CreatePayment() got = %+v, want successful payment", got)
	}
	if ledger.createCalls != 1 {
		t.Fatalf("expected create payment once, got %d", ledger.createCalls)
	}
	if ledger.requestedCalls != 1 {
		t.Fatalf("expected mark requested once, got %d", ledger.requestedCalls)
	}
	if ledger.callbackCalls != 1 {
		t.Fatalf("expected handle callback once, got %d", ledger.callbackCalls)
	}
	if adapter.requestCalls != 1 {
		t.Fatalf("expected adapter request once, got %d", adapter.requestCalls)
	}
	if hotWriter.updateCalls != 1 || hotWriter.orderID != "order-1" || hotWriter.status != "success" {
		t.Fatalf("expected one hot status success update, got calls=%d order=%s status=%s", hotWriter.updateCalls, hotWriter.orderID, hotWriter.status)
	}
	if hotWriter.advanceCalls != 1 {
		t.Fatalf("expected one reservation advancement, got %d", hotWriter.advanceCalls)
	}
	if hotWriter.advanceOrderID != "order-1" || hotWriter.advanceStatus != entity.ReservationStatusConsumed {
		t.Fatalf("expected reservation advancement to order-1/consumed, got order=%s status=%d", hotWriter.advanceOrderID, hotWriter.advanceStatus)
	}
	if hotWriter.advancePaymentID != "pay-1" {
		t.Fatalf("expected payment id pay-1 in reservation advancement, got %s", hotWriter.advancePaymentID)
	}
}

func TestServiceCreatePaymentReturnsExistingTerminalPayment(t *testing.T) {
	ledger := &fakeLedger{
		createResult: &CreatePaymentResult{
			Existing: true,
			Payment: &entity.Payment{
				PaymentId: "pay-1",
				OrderId:   "order-1",
				Status:    entity.PaymentStatusSuccess,
				RequestId: "req-1",
			},
		},
	}
	adapter := &fakeAdapter{}
	hotWriter := &fakeHotOrderWriter{}
	svc := NewService(ledger, adapter, hotWriter)

	got, err := svc.CreatePayment(context.Background(), &CreatePaymentInput{
		OrderID:   "order-1",
		Channel:   "mock_alipay",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("CreatePayment() error = %v", err)
	}

	if got == nil || got.PaymentId != "pay-1" {
		t.Fatalf("CreatePayment() got = %+v, want existing payment", got)
	}
	if adapter.requestCalls != 0 {
		t.Fatalf("expected adapter not to be called, got %d", adapter.requestCalls)
	}
	if ledger.requestedCalls != 0 {
		t.Fatalf("expected mark requested not to be called, got %d", ledger.requestedCalls)
	}
	if ledger.callbackCalls != 0 {
		t.Fatalf("expected callback not to be called, got %d", ledger.callbackCalls)
	}
	if hotWriter.updateCalls != 0 {
		t.Fatalf("expected hot status not to be updated, got %d", hotWriter.updateCalls)
	}
}

func TestServiceHandlePaymentCallbackDuplicateIsIdempotent(t *testing.T) {
	ledger := &fakeLedger{
		callbackResult: &HandlePaymentCallbackResult{
			Duplicate: true,
			Payment: &entity.Payment{
				PaymentId: "pay-1",
				OrderId:   "order-1",
				Status:    entity.PaymentStatusSuccess,
			},
		},
	}
	hotWriter := &fakeHotOrderWriter{}
	svc := NewService(ledger, &fakeAdapter{}, hotWriter)

	got, err := svc.HandlePaymentCallback(context.Background(), &HandlePaymentCallbackInput{
		PaymentID:         "pay-1",
		OrderID:           "order-1",
		CallbackID:        "cbk-1",
		Channel:           "mock_alipay",
		ThirdPartyTradeNo: "trade-1",
		RawPayload:        "{}",
		Operator:          "payment_callback",
	})
	if err != nil {
		t.Fatalf("HandlePaymentCallback() error = %v", err)
	}
	if got == nil || !got.Duplicate {
		t.Fatalf("HandlePaymentCallback() got = %+v, want duplicate result", got)
	}
	if hotWriter.updateCalls != 1 || hotWriter.orderID != "order-1" || hotWriter.status != "success" {
		t.Fatalf("expected duplicate callback to re-drive hot status sync, got calls=%d order=%s status=%s", hotWriter.updateCalls, hotWriter.orderID, hotWriter.status)
	}
	if hotWriter.advanceCalls != 1 || hotWriter.advanceStatus != entity.ReservationStatusConsumed {
		t.Fatalf("expected duplicate callback to re-drive reservation advancement, got calls=%d status=%d", hotWriter.advanceCalls, hotWriter.advanceStatus)
	}
}

func TestServiceGetPaymentDelegatesToLedger(t *testing.T) {
	ledger := &fakeLedger{
		getPaymentResult: &entity.Payment{
			PaymentId: "pay-9",
			OrderId:   "order-9",
			Status:    entity.PaymentStatusRequested,
		},
	}
	svc := NewService(ledger, &fakeAdapter{}, &fakeHotOrderWriter{})

	got, err := svc.GetPayment(context.Background(), "pay-9", "")
	if err != nil {
		t.Fatalf("GetPayment() error = %v", err)
	}
	if got == nil || got.PaymentId != "pay-9" {
		t.Fatalf("GetPayment() got = %+v, want pay-9", got)
	}
	if ledger.getPaymentCalls != 1 || ledger.getPaymentID != "pay-9" {
		t.Fatalf("expected get payment called with pay-9, got calls=%d id=%s", ledger.getPaymentCalls, ledger.getPaymentID)
	}
}

func TestServiceCreatePaymentPropagatesAdapterError(t *testing.T) {
	ledger := &fakeLedger{
		createResult: &CreatePaymentResult{
			Payment: &entity.Payment{
				PaymentId: "pay-1",
				OrderId:   "order-1",
				Status:    entity.PaymentStatusInit,
				RequestId: "req-1",
			},
		},
		requestedResult: &entity.Payment{
			PaymentId: "pay-1",
			OrderId:   "order-1",
			Status:    entity.PaymentStatusRequested,
			RequestId: "req-1",
		},
	}
	svc := NewService(ledger, &fakeAdapter{requestErr: errors.New("adapter down")}, &fakeHotOrderWriter{})

	_, err := svc.CreatePayment(context.Background(), &CreatePaymentInput{
		OrderID:   "order-1",
		Channel:   "mock_alipay",
		RequestID: "req-1",
	})
	if err == nil {
		t.Fatal("expected adapter error")
	}
	if ledger.callbackCalls != 0 {
		t.Fatalf("expected callback not to be called after adapter error, got %d", ledger.callbackCalls)
	}
}
