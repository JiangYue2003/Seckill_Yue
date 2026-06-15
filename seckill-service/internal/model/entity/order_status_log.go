package entity

type OrderStatusLog struct {
	ID         int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	OrderID    string `gorm:"column:order_id;type:varchar(64);not null;index:idx_order_status_log_order_id" json:"order_id"`
	FromStatus int32  `gorm:"column:from_status;not null" json:"from_status"`
	ToStatus   int32  `gorm:"column:to_status;not null" json:"to_status"`
	EventType  string `gorm:"column:event_type;type:varchar(64);not null;index:idx_order_status_log_event_type" json:"event_type"`
	Reason     string `gorm:"type:varchar(128)" json:"reason"`
	Operator   string `gorm:"type:varchar(64);not null;default:system" json:"operator"`
	TraceID    string `gorm:"column:trace_id;type:varchar(64)" json:"trace_id"`
	CreatedAt  int64  `gorm:"column:created_at;index:idx_order_status_log_created_at" json:"created_at"`
}

func (OrderStatusLog) TableName() string {
	return "order_status_logs"
}
