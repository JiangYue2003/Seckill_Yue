package model

import (
	"context"
	"errors"
	"time"

	"seckill-mall/order-service/internal/model/entity"

	"gorm.io/gorm"
)

type OutboxStore interface {
	ClaimPending(ctx context.Context, limit int) ([]entity.EventOutbox, error)
	MarkPublished(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, retryCount int32, nextRetryAt int64, lastErr string, dead bool) error
}

type outboxStore struct {
	db *gorm.DB
}

func NewOutboxStore(db *gorm.DB) OutboxStore {
	return &outboxStore{db: db}
}

func (s *outboxStore) ClaimPending(ctx context.Context, limit int) ([]entity.EventOutbox, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("outbox store db is nil")
	}
	if limit <= 0 {
		limit = 20
	}

	now := time.Now().Unix()
	var rows []entity.EventOutbox
	err := s.db.WithContext(ctx).
		Where("(status = ? OR status = ?) AND (next_retry_at = 0 OR next_retry_at <= ?)",
			entity.OutboxStatusNew,
			entity.OutboxStatusFailed,
			now,
		).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *outboxStore) MarkPublished(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return errors.New("outbox store db is nil")
	}
	now := time.Now().Unix()
	return s.db.WithContext(ctx).
		Model(&entity.EventOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        entity.OutboxStatusPublished,
			"last_error":    "",
			"next_retry_at": int64(0),
			"updated_at":    now,
		}).Error
}

func (s *outboxStore) MarkFailed(ctx context.Context, id int64, retryCount int32, nextRetryAt int64, lastErr string, dead bool) error {
	if s == nil || s.db == nil {
		return errors.New("outbox store db is nil")
	}
	now := time.Now().Unix()
	status := entity.OutboxStatusFailed
	if dead {
		status = entity.OutboxStatusDead
	}
	return s.db.WithContext(ctx).
		Model(&entity.EventOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        status,
			"retry_count":   retryCount,
			"next_retry_at": nextRetryAt,
			"last_error":    trimOutboxError(lastErr),
			"updated_at":    now,
		}).Error
}

func trimOutboxError(msg string) string {
	if len(msg) <= 512 {
		return msg
	}
	return msg[:512]
}
