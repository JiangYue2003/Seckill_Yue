package entity

type SeckillReservation struct {
	ReservationId    string `gorm:"primaryKey;type:varchar(64)" json:"reservation_id"`
	OrderId          string `gorm:"type:varchar(64);not null;uniqueIndex:uk_reservation_order_id" json:"order_id"`
	UserId           int64  `gorm:"not null;index:idx_reservation_user_spid,priority:1" json:"user_id"`
	SeckillProductId int64  `gorm:"not null;index:idx_reservation_user_spid,priority:2" json:"seckill_product_id"`
	ProductId        int64  `gorm:"not null" json:"product_id"`
	Quantity         int    `gorm:"not null;default:1" json:"quantity"`
	Amount           int64  `gorm:"not null" json:"amount"`
	Status           int32  `gorm:"not null;default:0;index:idx_reservation_status_expire_at,priority:1" json:"status"`
	Source           string `gorm:"type:varchar(32);not null;default:gateway" json:"source"`
	Reason           string `gorm:"type:varchar(64)" json:"reason"`
	RedisOrderKey    string `gorm:"column:redis_order_key;type:varchar(128)" json:"redis_order_key"`
	ExpireAt         int64  `gorm:"column:expire_at;index:idx_reservation_status_expire_at,priority:2" json:"expire_at"`
	CreatedAt        int64  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        int64  `gorm:"column:updated_at" json:"updated_at"`
}

func (SeckillReservation) TableName() string {
	return "seckill_reservations"
}
