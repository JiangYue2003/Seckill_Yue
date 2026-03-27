package entity

// SeckillOrder 秒杀订单记录
type SeckillOrder struct {
	ID               int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	UserId           int64  `gorm:"not null;uniqueIndex:uk_user_seckill" json:"user_id"`                  // 用户ID
	SeckillProductId int64  `gorm:"not null;uniqueIndex:uk_user_seckill;index" json:"seckill_product_id"` // 秒杀商品ID
	OrderId          string `gorm:"type:varchar(32);not null;index" json:"order_id"`                      // 订单号
	Quantity         int    `gorm:"not null;default:1" json:"quantity"`                                   // 购买数量
	CreatedAt        int64  `gorm:"column:created_at" json:"created_at"`                                  // 创建时间戳
}

// TableName 指定表名
func (SeckillOrder) TableName() string {
	return "seckill_orders"
}
