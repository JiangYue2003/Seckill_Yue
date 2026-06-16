package outbox

import (
	"context"
	"errors"
	"testing"

	"seckill-mall/order-service/internal/model/entity"
)

type fakeOutboxStore struct {
	rows          []entity.EventOutbox
	claimCalls    int
	claimLimit    int
	publishedIDs  []int64
	failedRecords []failedRecord
}

type failedRecord struct {
	id          int64
	retryCount  int32
	nextRetryAt int64
	lastErr     string
	dead        bool
}

func (f *fakeOutboxStore) ClaimPending(ctx context.Context, limit int) ([]entity.EventOutbox, error) {
	f.claimCalls++
	f.claimLimit = limit
	return f.rows, nil
}

func (f *fakeOutboxStore) MarkPublished(ctx context.Context, id int64) error {
	f.publishedIDs = append(f.publishedIDs, id)
	return nil
}

func (f *fakeOutboxStore) MarkFailed(ctx context.Context, id int64, retryCount int32, nextRetryAt int64, lastErr string, dead bool) error {
	f.failedRecords = append(f.failedRecords, failedRecord{
		id:          id,
		retryCount:  retryCount,
		nextRetryAt: nextRetryAt,
		lastErr:     lastErr,
		dead:        dead,
	})
	return nil
}

type fakePublisherProducer struct {
	published []entity.EventOutbox
	failIDs   map[int64]error
}

func (f *fakePublisherProducer) PublishEvent(ctx context.Context, event entity.EventOutbox) error {
	if err, ok := f.failIDs[event.ID]; ok {
		return err
	}
	f.published = append(f.published, event)
	return nil
}

func TestPublisherPublishOnceMarksPublishedOnSuccess(t *testing.T) {
	store := &fakeOutboxStore{
		rows: []entity.EventOutbox{
			{ID: 21, EventId: "evt-21", EventType: "payment.succeeded", PayloadJSON: `{"payment_id":"pay-21","order_id":"order-21"}`},
		},
	}
	producer := &fakePublisherProducer{}
	publisher := NewPublisher(store, producer)

	if err := publisher.PublishOnce(context.Background()); err != nil {
		t.Fatalf("PublishOnce() error = %v", err)
	}
	if len(producer.published) != 1 {
		t.Fatalf("expected one published event, got %d", len(producer.published))
	}
	if len(store.publishedIDs) != 1 || store.publishedIDs[0] != 21 {
		t.Fatalf("expected outbox id 21 marked published, got %+v", store.publishedIDs)
	}
	if len(store.failedRecords) != 0 {
		t.Fatalf("expected no failed records, got %+v", store.failedRecords)
	}
}

func TestPublisherPublishOnceMarksFailedAndRetry(t *testing.T) {
	store := &fakeOutboxStore{
		rows: []entity.EventOutbox{
			{ID: 22, EventId: "evt-22", EventType: "payment.succeeded", PayloadJSON: `{"payment_id":"pay-22","order_id":"order-22"}`},
		},
	}
	producer := &fakePublisherProducer{
		failIDs: map[int64]error{
			22: errors.New("rabbitmq unavailable"),
		},
	}
	publisher := NewPublisher(store, producer)

	if err := publisher.PublishOnce(context.Background()); err != nil {
		t.Fatalf("PublishOnce() error = %v", err)
	}
	if len(store.failedRecords) != 1 {
		t.Fatalf("expected one failed record, got %+v", store.failedRecords)
	}
	record := store.failedRecords[0]
	if record.id != 22 || record.retryCount != 1 || record.dead {
		t.Fatalf("unexpected failed record: %+v", record)
	}
	if record.nextRetryAt <= 0 {
		t.Fatalf("expected next retry time to be set, got %+v", record)
	}
}

func TestPublisherPublishOnceMarksDeadAfterMaxRetry(t *testing.T) {
	store := &fakeOutboxStore{
		rows: []entity.EventOutbox{
			{ID: 23, EventId: "evt-23", EventType: "order.completed", RetryCount: 4, PayloadJSON: `{"payment_id":"pay-23","order_id":"order-23"}`},
		},
	}
	producer := &fakePublisherProducer{
		failIDs: map[int64]error{
			23: errors.New("permanent error"),
		},
	}
	publisher := NewPublisher(store, producer)

	if err := publisher.PublishOnce(context.Background()); err != nil {
		t.Fatalf("PublishOnce() error = %v", err)
	}
	if len(store.failedRecords) != 1 {
		t.Fatalf("expected one failed record, got %+v", store.failedRecords)
	}
	record := store.failedRecords[0]
	if !record.dead || record.retryCount != 5 || record.nextRetryAt != 0 {
		t.Fatalf("expected dead-letter mark on max retry, got %+v", record)
	}
}
