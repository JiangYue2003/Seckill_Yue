package entity

type PaymentCallback struct {
	ID            int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	PaymentId     string `gorm:"column:payment_id;type:varchar(64);not null;index:idx_callback_payment_id" json:"payment_id"`
	OrderId       string `gorm:"column:order_id;type:varchar(64);not null;index:idx_callback_order_id" json:"order_id"`
	CallbackId    string `gorm:"column:callback_id;type:varchar(64);not null;uniqueIndex:uk_callback_id" json:"callback_id"`
	Channel       string `gorm:"column:channel;type:varchar(32);not null" json:"channel"`
	RawPayload    string `gorm:"column:raw_payload;type:json;not null" json:"raw_payload"`
	VerifyResult  int32  `gorm:"column:verify_result;not null;default:0" json:"verify_result"`
	ProcessResult int32  `gorm:"column:process_result;not null;default:0" json:"process_result"`
	ReceivedAt    int64  `gorm:"column:received_at;not null;index:idx_callback_received_at" json:"received_at"`
	CreatedAt     int64  `gorm:"column:created_at;not null" json:"created_at"`
}

func (PaymentCallback) TableName() string {
	return "payment_callbacks"
}

const (
	PaymentCallbackVerifyUnknown int32 = 0
	PaymentCallbackVerifyPass    int32 = 1
	PaymentCallbackVerifyFail    int32 = 2
)

const (
	PaymentCallbackProcessInit      int32 = 0
	PaymentCallbackProcessSucceeded int32 = 1
	PaymentCallbackProcessFailed    int32 = 2
	PaymentCallbackProcessDuplicate int32 = 3
)
