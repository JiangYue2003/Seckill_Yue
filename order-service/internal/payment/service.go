package payment

import (
	"context"
	"errors"

	"seckill-mall/order-service/internal/model/entity"
)

type Ledger interface {
	CreatePayment(ctx context.Context, in *CreatePaymentInput) (*CreatePaymentResult, error)
	MarkPaymentRequested(ctx context.Context, in *MarkPaymentRequestedInput) (*entity.Payment, error)
	HandlePaymentCallback(ctx context.Context, in *HandlePaymentCallbackInput) (*HandlePaymentCallbackResult, error)
	GetPayment(ctx context.Context, paymentID, orderID string) (*entity.Payment, error)
}

type Adapter interface {
	RequestPayment(ctx context.Context, in *RequestPaymentInput) (*RequestPaymentResult, error)
}

type HotOrderWriter interface {
	UpdateOrderStatus(ctx context.Context, orderID, status string, allowRecover bool) error
	AdvanceReservation(ctx context.Context, in *AdvanceReservationInput) error
}

type Service struct {
	ledger    Ledger
	adapter   Adapter
	hotWriter HotOrderWriter
}

func NewService(ledger Ledger, adapter Adapter, hotWriter HotOrderWriter) *Service {
	return &Service{
		ledger:    ledger,
		adapter:   adapter,
		hotWriter: hotWriter,
	}
}

func (s *Service) CreatePayment(ctx context.Context, in *CreatePaymentInput) (*entity.Payment, error) {
	if s == nil || s.ledger == nil || s.adapter == nil {
		return nil, errors.New("payment service dependency is nil")
	}
	created, err := s.ledger.CreatePayment(ctx, in)
	if err != nil {
		return nil, err
	}
	if created == nil || created.Payment == nil {
		return nil, errors.New("create payment returned nil payment")
	}
	if created.Existing && isTerminalPayment(created.Payment.Status) {
		return created.Payment, nil
	}

	paymentRecord, err := s.ledger.MarkPaymentRequested(ctx, &MarkPaymentRequestedInput{
		PaymentID: created.Payment.PaymentId,
		Channel:   created.Payment.Channel,
	})
	if err != nil {
		return nil, err
	}

	requestResult, err := s.adapter.RequestPayment(ctx, &RequestPaymentInput{
		PaymentID: paymentRecord.PaymentId,
		OrderID:   paymentRecord.OrderId,
		UserID:    paymentRecord.UserId,
		Amount:    paymentRecord.Amount,
		Channel:   paymentRecord.Channel,
		RequestID: paymentRecord.RequestId,
	})
	if err != nil {
		return nil, err
	}

	callbackResult, err := s.HandlePaymentCallback(ctx, &HandlePaymentCallbackInput{
		PaymentID:         paymentRecord.PaymentId,
		OrderID:           paymentRecord.OrderId,
		CallbackID:        requestResult.CallbackID,
		Channel:           paymentRecord.Channel,
		ThirdPartyTradeNo: requestResult.ThirdPartyTradeNo,
		RawPayload:        requestResult.RawPayload,
		Operator:          in.CallbackFrom,
	})
	if err != nil {
		return nil, err
	}
	if callbackResult == nil || callbackResult.Payment == nil {
		return nil, errors.New("handle payment callback returned nil payment")
	}
	return callbackResult.Payment, nil
}

func (s *Service) HandlePaymentCallback(ctx context.Context, in *HandlePaymentCallbackInput) (*HandlePaymentCallbackResult, error) {
	if s == nil || s.ledger == nil {
		return nil, errors.New("payment service ledger is nil")
	}
	result, err := s.ledger.HandlePaymentCallback(ctx, in)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Payment == nil {
		return nil, errors.New("payment callback result is nil")
	}
	if result.Payment.Status == entity.PaymentStatusSuccess && s.hotWriter != nil {
		if err := s.hotWriter.AdvanceReservation(ctx, &AdvanceReservationInput{
			OrderID:      result.Payment.OrderId,
			TargetStatus: entity.ReservationStatusConsumed,
			Reason:       "payment.succeeded",
			Operator:     defaultPaymentOperator(in),
			PaymentID:    result.Payment.PaymentId,
			AllowRecover: true,
		}); err != nil {
			return nil, err
		}
		if err := s.hotWriter.UpdateOrderStatus(ctx, result.Payment.OrderId, "success", true); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) GetPayment(ctx context.Context, paymentID, orderID string) (*entity.Payment, error) {
	if s == nil || s.ledger == nil {
		return nil, errors.New("payment service ledger is nil")
	}
	return s.ledger.GetPayment(ctx, paymentID, orderID)
}

func isTerminalPayment(status int32) bool {
	return status == entity.PaymentStatusSuccess ||
		status == entity.PaymentStatusClosed ||
		status == entity.PaymentStatusRefunded ||
		status == entity.PaymentStatusFailed
}

func defaultPaymentOperator(in *HandlePaymentCallbackInput) string {
	if in == nil || in.Operator == "" {
		return "payment_callback"
	}
	return in.Operator
}
