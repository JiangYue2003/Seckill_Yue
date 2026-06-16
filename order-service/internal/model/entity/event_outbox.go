package entity

type EventOutbox struct {
	ID            int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	EventId       string `gorm:"type:varchar(64);not null;uniqueIndex:uk_outbox_event_id" json:"event_id"`
	AggregateType string `gorm:"type:varchar(32);not null;index:idx_outbox_aggregate,priority:1" json:"aggregate_type"`
	AggregateId   string `gorm:"type:varchar(64);not null;index:idx_outbox_aggregate,priority:2" json:"aggregate_id"`
	EventType     string `gorm:"type:varchar(64);not null" json:"event_type"`
	PayloadJSON   string `gorm:"column:payload_json;type:json;not null" json:"payload_json"`
	Status        int32  `gorm:"not null;default:0;index:idx_outbox_status_next_retry,priority:1" json:"status"`
	RetryCount    int32  `gorm:"not null;default:0" json:"retry_count"`
	NextRetryAt   int64  `gorm:"column:next_retry_at;index:idx_outbox_status_next_retry,priority:2" json:"next_retry_at"`
	LastError     string `gorm:"type:varchar(512)" json:"last_error"`
	CreatedAt     int64  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt     int64  `gorm:"column:updated_at" json:"updated_at"`
}

func (EventOutbox) TableName() string {
	return "event_outbox"
}

const (
	OutboxStatusNew           int32 = 0
	OutboxStatusPublished     int32 = 1
	OutboxStatusConsumedAcked int32 = 2
	OutboxStatusFailed        int32 = 3
	OutboxStatusDead          int32 = 4
)
