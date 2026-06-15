package entity

type Payment struct {
	PaymentId         string `gorm:"column:payment_id;primaryKey;type:varchar(64)" json:"payment_id"`
	OrderId           string `gorm:"column:order_id;type:varchar(64);not null;uniqueIndex:uk_payment_order_id" json:"order_id"`
	UserId            int64  `gorm:"column:user_id;not null;index:idx_payment_user_id" json:"user_id"`
	Amount            int64  `gorm:"column:amount;not null" json:"amount"`
	Channel           string `gorm:"column:channel;type:varchar(32);not null" json:"channel"`
	Status            int32  `gorm:"column:status;not null;default:0;index:idx_payment_status" json:"status"`
	ThirdPartyTradeNo string `gorm:"column:third_party_trade_no;type:varchar(64)" json:"third_party_trade_no"`
	RequestId         string `gorm:"column:request_id;type:varchar(64);not null;uniqueIndex:uk_payment_request_id" json:"request_id"`
	PaidAt            int64  `gorm:"column:paid_at" json:"paid_at"`
	ClosedAt          int64  `gorm:"column:closed_at" json:"closed_at"`
	CreatedAt         int64  `gorm:"column:created_at;index:idx_payment_created_at" json:"created_at"`
	UpdatedAt         int64  `gorm:"column:updated_at" json:"updated_at"`
}

func (Payment) TableName() string {
	return "payments"
}

const (
	PaymentStatusInit      int32 = 0
	PaymentStatusRequested int32 = 1
	PaymentStatusSuccess   int32 = 2
	PaymentStatusFailed    int32 = 3
	PaymentStatusClosed    int32 = 4
	PaymentStatusRefunded  int32 = 5
)
