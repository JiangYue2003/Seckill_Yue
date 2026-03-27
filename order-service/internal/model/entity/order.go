package entity

// Order 订单实体
type Order struct {
	OrderId      string `gorm:"primaryKey;type:varchar(32)" json:"order_id"`    // 订单号
	UserId       int64  `gorm:"not null;index" json:"user_id"`                  // 用户ID
	ProductId    int64  `gorm:"not null;index" json:"product_id"`               // 商品ID
	ProductName  string `gorm:"type:varchar(200);not null" json:"product_name"` // 商品名称（冗余）
	Quantity     int    `gorm:"not null;default:1" json:"quantity"`             // 购买数量
	Amount       int64  `gorm:"not null" json:"amount"`                         // 实付金额（分）
	SeckillPrice int64  `gorm:"default:0" json:"seckill_price"`                 // 秒杀价格（分）
	OrderType    int32  `gorm:"not null;default:0" json:"order_type"`           // 订单类型: 0=普通订单, 1=秒杀订单
	Status       int32  `gorm:"not null;default:0;index" json:"status"`         // 订单状态: 0=待支付, 1=已支付, 2=已取消, 3=已退款, 4=已完成
	PaymentId    string `gorm:"type:varchar(64)" json:"payment_id"`             // 支付流水号
	PaidAt       int64  `gorm:"column:paid_at" json:"paid_at"`                  // 支付时间戳
	CreatedAt    int64  `gorm:"column:created_at;index" json:"created_at"`      // Unix 时间戳
	UpdatedAt    int64  `gorm:"column:updated_at" json:"updated_at"`            // Unix 时间戳
}

// TableName 指定表名
func (Order) TableName() string {
	return "orders"
}

// 订单状态常量
const (
	OrderStatusPending   = 0 // 待支付
	OrderStatusPaid      = 1 // 已支付
	OrderStatusCancelled = 2 // 已取消
	OrderStatusRefunded  = 3 // 已退款
	OrderStatusCompleted = 4 // 已完成
)

// 订单类型常量
const (
	OrderTypeNormal  = 0 // 普通订单
	OrderTypeSeckill = 1 // 秒杀订单
)
