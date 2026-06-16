package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"seckill-mall/common/events"
	"seckill-mall/seckill-service/internal/config"
	"seckill-mall/seckill-service/internal/model/entity"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type PersistReservationInput struct {
	ReservationID    string
	OrderID          string
	UserID           int64
	SeckillProductID int64
	ProductID        int64
	Quantity         int64
	Amount           int64
	SeckillPrice     int64
	Source           string
	Reason           string
	RedisOrderKey    string
	ExpireAt         int64
}

type PersistReservationResult struct {
	Reservation *entity.SeckillReservation
}

type ReleaseReservationInput struct {
	ReservationID string
	OrderID       string
	Reason        string
	TargetStatus  int32
	Operator      string
}

type AdvanceReservationInput struct {
	ReservationID string
	OrderID       string
	TargetStatus  int32
	Reason        string
	Operator      string
	PaymentID     string
	AllowRecover  bool
}

type ReservationLedger interface {
	PersistReservation(ctx context.Context, in *PersistReservationInput) (*PersistReservationResult, error)
	GetReservation(ctx context.Context, reservationID, orderID string) (*entity.SeckillReservation, error)
	FindReservationByUserProduct(ctx context.Context, userID, seckillProductID int64) (*entity.SeckillReservation, error)
	ReleaseReservation(ctx context.Context, in *ReleaseReservationInput) (*entity.SeckillReservation, error)
	AdvanceReservation(ctx context.Context, in *AdvanceReservationInput) (*entity.SeckillReservation, error)
}

func BuildUserProductKey(userID, seckillProductID int64) string {
	return fmt.Sprintf("%d:%d", userID, seckillProductID)
}

type reservationLedger struct {
	db *gorm.DB
}

func NewDB(c config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(c.MySQL.DataSource), &gorm.Config{
		Logger: newGormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}
	sqlDB.SetMaxIdleConns(20)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

func NewReservationLedger(c config.Config) (ReservationLedger, error) {
	db, err := NewDB(c)
	if err != nil {
		return nil, err
	}
	return NewReservationLedgerWithDB(db), nil
}

func NewReservationLedgerWithDB(db *gorm.DB) ReservationLedger {
	return &reservationLedger{db: db}
}

func (m *reservationLedger) PersistReservation(ctx context.Context, in *PersistReservationInput) (*PersistReservationResult, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("reservation ledger db is nil")
	}
	if in == nil || in.ReservationID == "" || in.OrderID == "" {
		return nil, ErrInvalidParams
	}

	now := time.Now().Unix()
	reservation := &entity.SeckillReservation{}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record := entity.SeckillReservation{
			ReservationId:    in.ReservationID,
			OrderId:          in.OrderID,
			UserId:           in.UserID,
			SeckillProductId: in.SeckillProductID,
			ProductId:        in.ProductID,
			Quantity:         int(in.Quantity),
			Amount:           in.Amount,
			Status:           entity.ReservationStatusReserved,
			Source:           in.Source,
			Reason:           in.Reason,
			RedisOrderKey:    in.RedisOrderKey,
			ExpireAt:         in.ExpireAt,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := tx.Create(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrAlreadyExists
			}
			return err
		}

		payload, err := events.BuildReservationCreatedPayload(events.ReservationCreatedInput{
			EventID:          "evt-reservation-created-" + in.ReservationID,
			EventType:        "reservation.created",
			OccurredAt:       now,
			AggregateID:      in.ReservationID,
			TraceID:          in.OrderID,
			Source:           in.Source,
			Version:          1,
			MessageID:        in.OrderID,
			ReservationID:    in.ReservationID,
			OrderID:          in.OrderID,
			UserID:           in.UserID,
			SeckillProductID: in.SeckillProductID,
			ProductID:        in.ProductID,
			Quantity:         in.Quantity,
			SeckillPrice:     in.SeckillPrice,
			Amount:           in.Amount,
			Status:           entity.ReservationStatusReserved,
			ExpireAt:         in.ExpireAt,
		})
		if err != nil {
			return err
		}

		outboxRows := []entity.EventOutbox{
			{
				EventId:       "evt-reservation-created-" + in.ReservationID,
				AggregateType: "reservation",
				AggregateId:   in.ReservationID,
				EventType:     "reservation.created",
				PayloadJSON:   string(payload),
				Status:        entity.OutboxStatusNew,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			{
				EventId:       "evt-reservation-timeout-check-" + in.ReservationID,
				AggregateType: "reservation",
				AggregateId:   in.ReservationID,
				EventType:     "reservation.timeout.check",
				PayloadJSON:   string(payload),
				Status:        entity.OutboxStatusNew,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
		}
		for _, outbox := range outboxRows {
			if err := tx.Create(&outbox).Error; err != nil {
				return err
			}
		}

		*reservation = record
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &PersistReservationResult{Reservation: reservation}, nil
}

func (m *reservationLedger) GetReservation(ctx context.Context, reservationID, orderID string) (*entity.SeckillReservation, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("reservation ledger db is nil")
	}
	var reservation entity.SeckillReservation
	query := m.db.WithContext(ctx).Model(&entity.SeckillReservation{})
	switch {
	case reservationID != "":
		query = query.Where("reservation_id = ?", reservationID)
	case orderID != "":
		query = query.Where("order_id = ?", orderID)
	default:
		return nil, ErrInvalidParams
	}
	if err := query.First(&reservation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &reservation, nil
}

func (m *reservationLedger) FindReservationByUserProduct(ctx context.Context, userID, seckillProductID int64) (*entity.SeckillReservation, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("reservation ledger db is nil")
	}
	var reservation entity.SeckillReservation
	if err := m.db.WithContext(ctx).
		Where("user_id = ? AND seckill_product_id = ?", userID, seckillProductID).
		Order("created_at DESC").
		First(&reservation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &reservation, nil
}

func (m *reservationLedger) ReleaseReservation(ctx context.Context, in *ReleaseReservationInput) (*entity.SeckillReservation, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("reservation ledger db is nil")
	}
	if in == nil {
		return nil, ErrInvalidParams
	}

	now := time.Now().Unix()
	var reservation entity.SeckillReservation
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&entity.SeckillReservation{})
		switch {
		case in.ReservationID != "":
			query = query.Where("reservation_id = ?", in.ReservationID)
		case in.OrderID != "":
			query = query.Where("order_id = ?", in.OrderID)
		default:
			return ErrInvalidParams
		}

		if err := query.First(&reservation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		fromStatus := reservation.Status
		reservation.Status = in.TargetStatus
		reservation.Reason = in.Reason
		reservation.UpdatedAt = now
		if err := tx.Save(&reservation).Error; err != nil {
			return err
		}

		statusLog := entity.OrderStatusLog{
			OrderID:    reservation.OrderId,
			FromStatus: fromStatus,
			ToStatus:   in.TargetStatus,
			EventType:  "reservation.released",
			Reason:     in.Reason,
			Operator:   in.Operator,
			CreatedAt:  now,
		}
		if err := tx.Create(&statusLog).Error; err != nil {
			return err
		}

		payload, err := events.BuildReservationReleasedPayload(events.ReservationReleasedInput{
			EventID:          fmt.Sprintf("evt-reservation-release-%s-%d", reservation.ReservationId, now),
			EventType:        "reservation.released",
			OccurredAt:       now,
			AggregateID:      reservation.ReservationId,
			TraceID:          reservation.OrderId,
			Source:           defaultReservationOperator(in.Operator),
			Version:          1,
			MessageID:        reservation.OrderId,
			ReservationID:    reservation.ReservationId,
			OrderID:          reservation.OrderId,
			UserID:           reservation.UserId,
			SeckillProductID: reservation.SeckillProductId,
			ProductID:        reservation.ProductId,
			Quantity:         int64(reservation.Quantity),
			Amount:           reservation.Amount,
			FromStatus:       fromStatus,
			Status:           in.TargetStatus,
			Reason:           in.Reason,
		})
		if err != nil {
			return err
		}

		outbox := entity.EventOutbox{
			EventId:       fmt.Sprintf("evt-reservation-release-%s-%d", reservation.ReservationId, now),
			AggregateType: "reservation",
			AggregateId:   reservation.ReservationId,
			EventType:     "reservation.released",
			PayloadJSON:   string(payload),
			Status:        entity.OutboxStatusNew,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		return tx.Create(&outbox).Error
	})
	if err != nil {
		return nil, err
	}

	return &reservation, nil
}

func (m *reservationLedger) AdvanceReservation(ctx context.Context, in *AdvanceReservationInput) (*entity.SeckillReservation, error) {
	if m == nil || m.db == nil {
		return nil, errors.New("reservation ledger db is nil")
	}
	if in == nil || (in.ReservationID == "" && in.OrderID == "") {
		return nil, ErrInvalidParams
	}

	now := time.Now().Unix()
	var reservation entity.SeckillReservation
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&entity.SeckillReservation{})
		switch {
		case in.ReservationID != "":
			query = query.Where("reservation_id = ?", in.ReservationID)
		case in.OrderID != "":
			query = query.Where("order_id = ?", in.OrderID)
		default:
			return ErrInvalidParams
		}

		if err := query.First(&reservation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		fromStatus := reservation.Status
		if fromStatus == in.TargetStatus {
			return nil
		}
		if !canAdvanceReservationStatus(fromStatus, in.TargetStatus, in.AllowRecover) {
			return ErrInvalidParams
		}

		reservation.Status = in.TargetStatus
		reservation.Reason = in.Reason
		reservation.UpdatedAt = now
		if err := tx.Save(&reservation).Error; err != nil {
			return err
		}

		statusLog := entity.OrderStatusLog{
			OrderID:    reservation.OrderId,
			FromStatus: fromStatus,
			ToStatus:   in.TargetStatus,
			EventType:  "reservation.advanced",
			Reason:     in.Reason,
			Operator:   defaultReservationOperator(in.Operator),
			CreatedAt:  now,
		}
		if err := tx.Create(&statusLog).Error; err != nil {
			return err
		}

		payload, err := events.BuildReservationAdvancedPayload(events.ReservationAdvancedInput{
			EventID:          fmt.Sprintf("evt-reservation-advanced-%s-%d", reservation.ReservationId, now),
			EventType:        "reservation.advanced",
			OccurredAt:       now,
			AggregateID:      reservation.ReservationId,
			TraceID:          reservation.OrderId,
			Source:           defaultReservationOperator(in.Operator),
			Version:          1,
			MessageID:        reservation.OrderId,
			ReservationID:    reservation.ReservationId,
			OrderID:          reservation.OrderId,
			PaymentID:        in.PaymentID,
			UserID:           reservation.UserId,
			SeckillProductID: reservation.SeckillProductId,
			ProductID:        reservation.ProductId,
			Quantity:         int64(reservation.Quantity),
			Amount:           reservation.Amount,
			FromStatus:       fromStatus,
			Status:           in.TargetStatus,
			Reason:           in.Reason,
		})
		if err != nil {
			return err
		}

		outbox := entity.EventOutbox{
			EventId:       fmt.Sprintf("evt-reservation-advanced-%s-%d", reservation.ReservationId, now),
			AggregateType: "reservation",
			AggregateId:   reservation.ReservationId,
			EventType:     "reservation.advanced",
			PayloadJSON:   string(payload),
			Status:        entity.OutboxStatusNew,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		return tx.Create(&outbox).Error
	})
	if err != nil {
		return nil, err
	}

	return &reservation, nil
}

func canAdvanceReservationStatus(fromStatus, toStatus int32, allowRecover bool) bool {
	if fromStatus == toStatus {
		return true
	}
	if allowRecover && fromStatus == entity.ReservationStatusFailed &&
		(toStatus == entity.ReservationStatusPaid || toStatus == entity.ReservationStatusConsumed) {
		return true
	}
	return toStatus >= fromStatus
}

func defaultReservationOperator(operator string) string {
	if operator == "" {
		return "system"
	}
	return operator
}
