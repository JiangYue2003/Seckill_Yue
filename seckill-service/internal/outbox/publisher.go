package outbox

import (
	"context"
	"errors"
	"time"

	"seckill-mall/seckill-service/internal/metrics"
	"seckill-mall/seckill-service/internal/model"
	"seckill-mall/seckill-service/internal/model/entity"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultBatchSize   = 20
	defaultMaxRetry    = 5
	defaultRetryBase   = 2 * time.Second
	defaultPollInterval = time.Second
)

type PublisherProducer interface {
	PublishEvent(ctx context.Context, event entity.EventOutbox) error
}

type Publisher struct {
	store        model.OutboxStore
	producer     PublisherProducer
	batchSize    int
	maxRetry     int32
	retryBase    time.Duration
	pollInterval time.Duration
}

func NewPublisher(store model.OutboxStore, producer PublisherProducer) *Publisher {
	return &Publisher{
		store:        store,
		producer:     producer,
		batchSize:    defaultBatchSize,
		maxRetry:     defaultMaxRetry,
		retryBase:    defaultRetryBase,
		pollInterval: defaultPollInterval,
	}
}

func (p *Publisher) Run(ctx context.Context) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.PublishOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logx.Errorf("outbox publisher publish once failed: %v", err)
			}
		}
	}
}

func (p *Publisher) PublishOnce(ctx context.Context) error {
	if p == nil || p.store == nil || p.producer == nil {
		return nil
	}

	rows, err := p.store.ClaimPending(ctx, p.batchSize)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := p.producer.PublishEvent(ctx, row); err != nil {
			retryCount := row.RetryCount + 1
			dead := retryCount >= p.maxRetry
			nextRetryAt := time.Now().Add(p.retryDelay(retryCount)).Unix()
			if dead {
				nextRetryAt = 0
			}
			if markErr := p.store.MarkFailed(ctx, row.ID, retryCount, nextRetryAt, err.Error(), dead); markErr != nil {
				return markErr
			}
			metrics.SeckillMQEnqueueTotal.WithLabelValues("outbox", "failed").Inc()
			continue
		}
		if err := p.store.MarkPublished(ctx, row.ID); err != nil {
			return err
		}
		metrics.SeckillMQEnqueueTotal.WithLabelValues("outbox", "published").Inc()
	}
	return nil
}

func (p *Publisher) retryDelay(retryCount int32) time.Duration {
	if retryCount <= 1 {
		return p.retryBase
	}
	delay := p.retryBase * time.Duration(1<<uint(retryCount-1))
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}
