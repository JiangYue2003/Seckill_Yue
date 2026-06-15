package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"seckill-mall/common/utils"
	"seckill-mall/order-service/internal/model/entity"
	"seckill-mall/order-service/internal/payment"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PaymentLedger interface {
	CreatePayment(ctx context.Context, in *payment.CreatePaymentInput) (*payment.CreatePaymentResult, error)
	MarkPaymentRequested(ctx context.Context, in *payment.MarkPaymentRequestedInput) (*entity.Payment, error)
	HandlePaymentCallback(ctx context.Context, in *payment.HandlePaymentCallbackInput) (*payment.HandlePaymentCallbackResult, error)
	GetPayment(ctx context.Context, paymentID, orderID string) (*entity.Payment, error)
}

type paymentLedger struct {
	db *gorm.DB
}

func NewPaymentLedger(db *gorm.DB) PaymentLedger {
	return &paymentLedger{db: db}
}

func (m *paymentLedger) CreatePayment(ctx context.Context, in *payment.CreatePaymentInput) (*payment.CreatePaymentResult, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("payment ledger db is nil")
	}
	if in == nil || in.OrderID == "" || in.RequestID == "" {
		return nil, ErrInvalidParams
	}

	var result payment.CreatePaymentResult
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing entity.Payment
		err := tx.Where("request_id = ?", in.RequestID).First(&existing).Error
		switch {
		case err == nil:
			result.Payment = &existing
			result.Existing = true
			return nil
		case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}

		var orderRecord entity.Order
		if err := tx.Where("order_id = ?", in.OrderID).First(&orderRecord).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if orderRecord.Status != entity.OrderStatusOrderCreated && orderRecord.Status != entity.OrderStatusPaying {
			return ErrOrderCannotPay
		}

		now := time.Now().Unix()
		channel := in.Channel
		if channel == "" {
			channel = "mock_alipay"
		}
		paymentID := utils.GenerateOrderId("P")
		paymentRecord := entity.Payment{
			PaymentId: paymentID,
			OrderId:   orderRecord.OrderId,
			UserId:    orderRecord.UserId,
			Amount:    orderRecord.Amount,
			Channel:   channel,
			Status:    entity.PaymentStatusInit,
			RequestId: in.RequestID,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := tx.Create(&paymentRecord).Error; err != nil {
			if isDuplicateError(err) {
				var duplicate entity.Payment
				if findErr := tx.Where("request_id = ?", in.RequestID).First(&duplicate).Error; findErr == nil {
					result.Payment = &duplicate
					result.Existing = true
					return nil
				}
			}
			return err
		}

		result.Payment = &paymentRecord
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *paymentLedger) MarkPaymentRequested(ctx context.Context, in *payment.MarkPaymentRequestedInput) (*entity.Payment, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("payment ledger db is nil")
	}
	if in == nil || in.PaymentID == "" {
		return nil, ErrInvalidParams
	}

	var result entity.Payment
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var paymentRecord entity.Payment
		if err := tx.Where("payment_id = ?", in.PaymentID).First(&paymentRecord).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if paymentRecord.Status == entity.PaymentStatusRequested || paymentRecord.Status == entity.PaymentStatusSuccess {
			result = paymentRecord
			return nil
		}

		now := time.Now().Unix()
		if err := tx.Model(&entity.Payment{}).
			Where("payment_id = ? AND status = ?", in.PaymentID, entity.PaymentStatusInit).
			Updates(map[string]any{
				"status":     entity.PaymentStatusRequested,
				"channel":    choosePaymentChannel(in.Channel, paymentRecord.Channel),
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&entity.Order{}).
			Where("order_id = ?", paymentRecord.OrderId).
			Updates(map[string]any{
				"status":     entity.OrderStatusPaying,
				"pay_status": entity.OrderPayStatusRequested,
				"payment_id": paymentRecord.PaymentId,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&entity.SeckillReservation{}).
			Where("order_id = ?", paymentRecord.OrderId).
			Updates(map[string]any{
				"status":     entity.ReservationStatusPaying,
				"reason":     "payment.requested",
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Create(&entity.OrderStatusLog{
			OrderID:    paymentRecord.OrderId,
			FromStatus: entity.OrderStatusOrderCreated,
			ToStatus:   entity.OrderStatusPaying,
			EventType:  "payment.requested",
			Reason:     "payment requested",
			Operator:   "payment",
			CreatedAt:  now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("payment_id = ?", in.PaymentID).First(&result).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *paymentLedger) HandlePaymentCallback(ctx context.Context, in *payment.HandlePaymentCallbackInput) (*payment.HandlePaymentCallbackResult, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("payment ledger db is nil")
	}
	if in == nil || in.PaymentID == "" || in.CallbackID == "" {
		return nil, ErrInvalidParams
	}

	result := &payment.HandlePaymentCallbackResult{}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var duplicate entity.PaymentCallback
		err := tx.Where("callback_id = ?", in.CallbackID).First(&duplicate).Error
		switch {
		case err == nil:
			result.Duplicate = true
			paymentRecord, getErr := m.getPaymentTx(tx, in.PaymentID, in.OrderID)
			if getErr != nil {
				return getErr
			}
			result.Payment = paymentRecord
			return nil
		case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}

		paymentRecord, err := m.getPaymentTx(tx, in.PaymentID, in.OrderID)
		if err != nil {
			return err
		}

		now := time.Now().Unix()
		callback := entity.PaymentCallback{
			PaymentId:     paymentRecord.PaymentId,
			OrderId:       paymentRecord.OrderId,
			CallbackId:    in.CallbackID,
			Channel:       choosePaymentChannel(in.Channel, paymentRecord.Channel),
			RawPayload:    normalizeCallbackPayload(in.RawPayload),
			VerifyResult:  entity.PaymentCallbackVerifyPass,
			ProcessResult: entity.PaymentCallbackProcessSucceeded,
			ReceivedAt:    now,
			CreatedAt:     now,
		}
		if err := tx.Create(&callback).Error; err != nil {
			if isDuplicateError(err) {
				result.Duplicate = true
				result.Payment = paymentRecord
				return nil
			}
			return err
		}

		if paymentRecord.Status == entity.PaymentStatusSuccess {
			result.Payment = paymentRecord
			return nil
		}

		if err := tx.Model(&entity.Payment{}).
			Where("payment_id = ?", paymentRecord.PaymentId).
			Updates(map[string]any{
				"status":               entity.PaymentStatusSuccess,
				"third_party_trade_no": in.ThirdPartyTradeNo,
				"paid_at":              now,
				"updated_at":           now,
			}).Error; err != nil {
			return err
		}

		if err := tx.Model(&entity.Order{}).
			Where("order_id = ?", paymentRecord.OrderId).
			Updates(map[string]any{
				"status":     entity.OrderStatusCompleted,
				"pay_status": entity.OrderPayStatusSuccess,
				"payment_id": paymentRecord.PaymentId,
				"paid_at":    now,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		if err := tx.Model(&entity.SeckillReservation{}).
			Where("order_id = ?", paymentRecord.OrderId).
			Updates(map[string]any{
				"status":     entity.ReservationStatusConsumed,
				"reason":     "payment.succeeded",
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		logs := []entity.OrderStatusLog{
			{
				OrderID:    paymentRecord.OrderId,
				FromStatus: entity.OrderStatusPaying,
				ToStatus:   entity.OrderStatusPaid,
				EventType:  "payment.succeeded",
				Reason:     "payment callback succeeded",
				Operator:   defaultOperator(in.Operator),
				CreatedAt:  now,
			},
			{
				OrderID:    paymentRecord.OrderId,
				FromStatus: entity.OrderStatusPaid,
				ToStatus:   entity.OrderStatusCompleted,
				EventType:  "order.completed",
				Reason:     "mock payment completes immediately",
				Operator:   defaultOperator(in.Operator),
				CreatedAt:  now,
			},
		}
		if err := tx.Create(&logs).Error; err != nil {
			return err
		}

		paymentPayload, err := json.Marshal(map[string]any{
			"payment_id":           paymentRecord.PaymentId,
			"order_id":             paymentRecord.OrderId,
			"status":               entity.PaymentStatusSuccess,
			"third_party_trade_no": in.ThirdPartyTradeNo,
			"paid_at":              now,
		})
		if err != nil {
			return err
		}
		completedPayload, err := json.Marshal(map[string]any{
			"payment_id": paymentRecord.PaymentId,
			"order_id":   paymentRecord.OrderId,
			"status":     entity.OrderStatusCompleted,
			"paid_at":    now,
		})
		if err != nil {
			return err
		}

		outboxRows := []entity.EventOutbox{
			{
				EventId:       fmt.Sprintf("evt-payment-succeeded-%s", in.CallbackID),
				AggregateType: "payment",
				AggregateId:   paymentRecord.PaymentId,
				EventType:     "payment.succeeded",
				PayloadJSON:   string(paymentPayload),
				Status:        outboxStatusNew,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			{
				EventId:       fmt.Sprintf("evt-order-completed-%s", paymentRecord.OrderId),
				AggregateType: "order",
				AggregateId:   paymentRecord.OrderId,
				EventType:     "order.completed",
				PayloadJSON:   string(completedPayload),
				Status:        outboxStatusNew,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
		}
		for _, outboxRow := range outboxRows {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&outboxRow).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("payment_id = ?", paymentRecord.PaymentId).First(paymentRecord).Error; err != nil {
			return err
		}
		result.Payment = paymentRecord
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (m *paymentLedger) GetPayment(ctx context.Context, paymentID, orderID string) (*entity.Payment, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("payment ledger db is nil")
	}
	return m.getPaymentTx(m.db.WithContext(ctx), paymentID, orderID)
}

func (m *paymentLedger) getPaymentTx(tx *gorm.DB, paymentID, orderID string) (*entity.Payment, error) {
	var paymentRecord entity.Payment
	query := tx.Model(&entity.Payment{})
	switch {
	case paymentID != "":
		query = query.Where("payment_id = ?", paymentID)
	case orderID != "":
		query = query.Where("order_id = ?", orderID)
	default:
		return nil, ErrInvalidParams
	}
	if err := query.First(&paymentRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &paymentRecord, nil
}

func choosePaymentChannel(incoming, fallback string) string {
	if incoming != "" {
		return incoming
	}
	if fallback != "" {
		return fallback
	}
	return "mock_alipay"
}

func normalizeCallbackPayload(raw string) string {
	if raw == "" {
		return "{}"
	}
	return raw
}

func defaultOperator(operator string) string {
	if operator == "" {
		return "payment_callback"
	}
	return operator
}
