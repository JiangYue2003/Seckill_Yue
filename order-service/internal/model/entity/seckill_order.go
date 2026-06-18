package entity

// SeckillOrder 秒杀订单记录
type SeckillOrder struct {
	ID               int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	UserId           int64  `gorm:"not null;uniqueIndex:uk_user_seckill" json:"user_id"`
	SeckillProductId int64  `gorm:"not null;uniqueIndex:uk_user_seckill;index" json:"seckill_product_id"`
	OrderId          string `gorm:"type:varchar(64);not null;uniqueIndex:uk_seckill_order_id;index" json:"order_id"`
	ReservationId    string `gorm:"column:reservation_id;type:varchar(64);index" json:"reservation_id"`
	Quantity         int    `gorm:"not null;default:1" json:"quantity"`
	ShardNo          int32  `gorm:"column:shard_no;not null;default:0;index" json:"shard_no"`
	Status           int32  `gorm:"not null;default:0;index" json:"status"`
	CreatedAt        int64  `gorm:"column:created_at" json:"created_at"`
}

// TableName 指定表名
func (SeckillOrder) TableName() string {
	return "seckill_orders"
}
