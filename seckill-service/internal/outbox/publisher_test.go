package outbox

import (
	"context"
	"errors"
	"testing"

	"seckill-mall/seckill-service/internal/model/entity"
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
			{ID: 11, EventId: "evt-11", EventType: "reservation.created", PayloadJSON: `{"message_id":"S11","order_id":"S11"}`},
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
	if len(store.publishedIDs) != 1 || store.publishedIDs[0] != 11 {
		t.Fatalf("expected outbox id 11 marked published, got %+v", store.publishedIDs)
	}
	if len(store.failedRecords) != 0 {
		t.Fatalf("expected no failed records, got %+v", store.failedRecords)
	}
}

func TestPublisherPublishOnceMarksFailedAndRetry(t *testing.T) {
	store := &fakeOutboxStore{
		rows: []entity.EventOutbox{
			{ID: 12, EventId: "evt-12", EventType: "reservation.created", PayloadJSON: `{"message_id":"S12","order_id":"S12"}`},
		},
	}
	producer := &fakePublisherProducer{
		failIDs: map[int64]error{
			12: errors.New("rabbitmq unavailable"),
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
	if record.id != 12 || record.retryCount != 1 || record.dead {
		t.Fatalf("unexpected failed record: %+v", record)
	}
	if record.nextRetryAt <= 0 {
		t.Fatalf("expected next retry time to be set, got %+v", record)
	}
}

func TestPublisherPublishOnceMarksDeadAfterMaxRetry(t *testing.T) {
	store := &fakeOutboxStore{
		rows: []entity.EventOutbox{
			{ID: 13, EventId: "evt-13", EventType: "reservation.created", RetryCount: 4, PayloadJSON: `{"message_id":"S13","order_id":"S13"}`},
		},
	}
	producer := &fakePublisherProducer{
		failIDs: map[int64]error{
			13: errors.New("permanent error"),
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
