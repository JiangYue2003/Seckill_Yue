package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type MockAdapter struct{}

func NewMockAdapter() *MockAdapter {
	return &MockAdapter{}
}

func (a *MockAdapter) RequestPayment(ctx context.Context, in *RequestPaymentInput) (*RequestPaymentResult, error) {
	if in == nil || in.PaymentID == "" || in.OrderID == "" {
		return nil, errors.New("invalid mock payment request")
	}
	callbackID := fmt.Sprintf("mock-cb-%s-%d", in.PaymentID, time.Now().UnixNano())
	tradeNo := fmt.Sprintf("mock-trade-%s", in.PaymentID)
	payload, err := json.Marshal(map[string]any{
		"payment_id":           in.PaymentID,
		"order_id":             in.OrderID,
		"channel":              in.Channel,
		"request_id":           in.RequestID,
		"third_party_trade_no": tradeNo,
		"callback_id":          callbackID,
		"status":               "success",
	})
	if err != nil {
		return nil, err
	}
	return &RequestPaymentResult{
		ThirdPartyTradeNo: tradeNo,
		CallbackID:        callbackID,
		RawPayload:        string(payload),
	}, nil
}
