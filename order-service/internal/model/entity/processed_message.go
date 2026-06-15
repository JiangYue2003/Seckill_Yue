package entity

type ProcessedMessage struct {
	ID           int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	MessageID    string `gorm:"column:message_id;type:varchar(64);not null;uniqueIndex:uk_message_consumer,priority:1" json:"message_id"`
	ConsumerName string `gorm:"column:consumer_name;type:varchar(64);not null;uniqueIndex:uk_message_consumer,priority:2" json:"consumer_name"`
	Status       int32  `gorm:"not null;default:0;index:idx_processed_status" json:"status"`
	ProcessedAt  int64  `gorm:"column:processed_at" json:"processed_at"`
	CreatedAt    int64  `gorm:"column:created_at" json:"created_at"`
}

func (ProcessedMessage) TableName() string {
	return "processed_messages"
}
