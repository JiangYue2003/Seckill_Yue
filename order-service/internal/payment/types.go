package payment

import "seckill-mall/order-service/internal/model/entity"

type CreatePaymentInput struct {
	OrderID      string
	Channel      string
	RequestID    string
	Operator     string
	CallbackFrom string
}

type CreatePaymentResult struct {
	Payment  *entity.Payment
	Existing bool
}

type MarkPaymentRequestedInput struct {
	PaymentID string
	Channel   string
}

type HandlePaymentCallbackInput struct {
	PaymentID         string
	OrderID           string
	CallbackID        string
	Channel           string
	ThirdPartyTradeNo string
	RawPayload        string
	Operator          string
}

type HandlePaymentCallbackResult struct {
	Payment   *entity.Payment
	Duplicate bool
}

type RequestPaymentInput struct {
	PaymentID string
	OrderID   string
	UserID    int64
	Amount    int64
	Channel   string
	RequestID string
}

type RequestPaymentResult struct {
	ThirdPartyTradeNo string
	CallbackID        string
	RawPayload        string
}
