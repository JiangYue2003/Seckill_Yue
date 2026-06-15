package entity

// Order 订单实体
type Order struct {
	OrderId       string `gorm:"primaryKey;type:varchar(64)" json:"order_id"`
	ReservationId string `gorm:"column:reservation_id;type:varchar(64);index" json:"reservation_id"`
	UserId        int64  `gorm:"not null;index" json:"user_id"`
	ProductId     int64  `gorm:"not null;index" json:"product_id"`
	ProductName   string `gorm:"type:varchar(200);not null" json:"product_name"`
	Quantity      int    `gorm:"not null;default:1" json:"quantity"`
	Amount        int64  `gorm:"not null" json:"amount"`
	SeckillPrice  int64  `gorm:"default:0" json:"seckill_price"`
	OrderType     int32  `gorm:"not null;default:0" json:"order_type"`
	Status        int32  `gorm:"not null;default:0;index" json:"status"`
	PayStatus     int32  `gorm:"column:pay_status;not null;default:0;index" json:"pay_status"`
	PaymentId     string `gorm:"type:varchar(64)" json:"payment_id"`
	PaidAt        int64  `gorm:"column:paid_at" json:"paid_at"`
	ClosedAt      int64  `gorm:"column:closed_at" json:"closed_at"`
	ExpiredAt     int64  `gorm:"column:expired_at" json:"expired_at"`
	Version       int32  `gorm:"not null;default:0" json:"version"`
	CreatedAt     int64  `gorm:"column:created_at;index" json:"created_at"`
	UpdatedAt     int64  `gorm:"column:updated_at" json:"updated_at"`
}

// TableName 指定表名
func (Order) TableName() string {
	return "orders"
}

// 订单状态常量
const (
	OrderStatusInit         = 0
	OrderStatusReserved     = 1
	OrderStatusOrderCreated = 2
	OrderStatusPaying       = 3
	OrderStatusPaid         = 4
	OrderStatusCompleted    = 5
	OrderStatusCancelled    = 6
	OrderStatusExpired      = 7
	OrderStatusFailed       = 8
	OrderStatusRefunded     = 9
)

// 订单类型常量
const (
	OrderTypeNormal  = 0 // 普通订单
	OrderTypeSeckill = 1 // 秒杀订单
)

const (
	OrderPayStatusInit      = 0
	OrderPayStatusRequested = 1
	OrderPayStatusSuccess   = 2
	OrderPayStatusFailed    = 3
	OrderPayStatusClosed    = 4
	OrderPayStatusRefunded  = 5
)
