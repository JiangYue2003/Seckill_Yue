package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"seckill-mall/order-service/internal/model/entity"

	"gorm.io/gorm"
)

const (
	processedMessageStatusProcessing = 0
	processedMessageStatusSucceeded  = 1

	seckillOrderRecordStatusReserved     = 0
	seckillOrderRecordStatusOrderCreated = 1
	seckillReservationStatusOrderCreated = 2
)

type seckillOrderTxManager struct {
	db *gorm.DB
}

func NewSeckillOrderTxManager(db *gorm.DB) SeckillOrderTxManager {
	return &seckillOrderTxManager{db: db}
}

func (m *seckillOrderTxManager) PersistSeckillOrder(ctx context.Context, in *PersistSeckillOrderInput) (*PersistSeckillOrderResult, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("tx manager db is nil")
	}
	if in == nil {
		return nil, ErrInvalidParams
	}
	if in.OrderID == "" || in.MessageID == "" || in.ConsumerName == "" {
		return nil, ErrInvalidParams
	}

	now := time.Now().Unix()
	result := &PersistSeckillOrderResult{}

	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var processed entity.ProcessedMessage
		err := tx.Where("message_id = ? AND consumer_name = ?", in.MessageID, in.ConsumerName).First(&processed).Error
		switch {
		case err == nil:
			result.AlreadyProcessed = true
			return nil
		case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}

		processed = entity.ProcessedMessage{
			MessageID:    in.MessageID,
			ConsumerName: in.ConsumerName,
			Status:       processedMessageStatusProcessing,
			CreatedAt:    now,
		}
		if err := tx.Create(&processed).Error; err != nil {
			if isDuplicateError(err) {
				result.AlreadyProcessed = true
				return nil
			}
			return err
		}

		orderRecord := entity.Order{
			OrderId:       in.OrderID,
			ReservationId: in.OrderID,
			UserId:        in.UserID,
			ProductId:     in.ProductID,
			ProductName:   "",
			Quantity:      int(in.Quantity),
			Amount:        in.Amount,
			SeckillPrice:  in.SeckillPrice,
			OrderType:     entity.OrderTypeSeckill,
			Status:        entity.OrderStatusOrderCreated,
			PayStatus:     entity.OrderPayStatusInit,
			ExpiredAt:     now + 300,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := tx.Create(&orderRecord).Error; err != nil {
			return err
		}

		seckillRecord := entity.SeckillOrder{
			UserId:           in.UserID,
			SeckillProductId: in.SeckillProductID,
			OrderId:          in.OrderID,
			ReservationId:    in.OrderID,
			Quantity:         int(in.Quantity),
			Status:           seckillOrderRecordStatusOrderCreated,
			CreatedAt:        now,
		}
		if err := tx.Create(&seckillRecord).Error; err != nil {
			return err
		}

		reservationRecord := entity.SeckillReservation{
			ReservationId:    in.OrderID,
			OrderId:          in.OrderID,
			UserId:           in.UserID,
			SeckillProductId: in.SeckillProductID,
			ProductId:        in.ProductID,
			Quantity:         int(in.Quantity),
			Amount:           in.Amount,
			Status:           seckillReservationStatusOrderCreated,
			Source:           "order-service-consumer",
			Reason:           "order.created",
			RedisOrderKey:    fmt.Sprintf("seckill:order:%s", in.OrderID),
			ExpireAt:         now + 300,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.Clauses().Create(&reservationRecord).Error; err != nil {
			return err
		}

		statusLog := entity.OrderStatusLog{
			OrderID:    in.OrderID,
			FromStatus: entity.OrderStatusReserved,
			ToStatus:   entity.OrderStatusOrderCreated,
			EventType:  "order.created",
			Reason:     "consume seckill order message",
			Operator:   "system",
			CreatedAt:  now,
		}
		if err := tx.Create(&statusLog).Error; err != nil {
			return err
		}

		payload, err := json.Marshal(map[string]any{
			"event_id":           "evt-" + in.MessageID,
			"order_id":           in.OrderID,
			"reservation_id":     in.OrderID,
			"user_id":            in.UserID,
			"seckill_product_id": in.SeckillProductID,
			"product_id":         in.ProductID,
			"quantity":           in.Quantity,
			"amount":             in.Amount,
			"status":             entity.OrderStatusOrderCreated,
			"occurred_at":        now,
		})
		if err != nil {
			return err
		}

		outbox := entity.EventOutbox{
			EventId:       "evt-" + in.MessageID,
			AggregateType: "order",
			AggregateId:   in.OrderID,
			EventType:     "order.created",
			PayloadJSON:   string(payload),
			Status:        entity.OutboxStatusNew,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := tx.Create(&outbox).Error; err != nil {
			return err
		}

		if err := tx.Model(&entity.ProcessedMessage{}).
			Where("id = ?", processed.ID).
			Updates(map[string]any{
				"status":       processedMessageStatusSucceeded,
				"processed_at": now,
			}).Error; err != nil {
			return err
		}

		result.OrderPersisted = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey)
}
