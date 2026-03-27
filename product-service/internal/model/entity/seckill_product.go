package entity

// SeckillProduct 秒杀商品实体
type SeckillProduct struct {
	ID           int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductId    int64 `gorm:"not null;uniqueIndex" json:"product_id"`        // 关联商品ID
	SeckillPrice int64 `gorm:"not null" json:"seckill_price"`                 // 秒杀价格（分）
	SeckillStock int   `gorm:"not null" json:"seckill_stock"`                 // 秒杀库存
	SoldCount    int   `gorm:"not null;default:0" json:"sold_count"`          // 已售数量
	StartTime    int64 `gorm:"not null" json:"start_time"`                    // 开始时间戳
	EndTime      int64 `gorm:"not null" json:"end_time"`                      // 结束时间戳
	PerLimit     int   `gorm:"not null;default:1" json:"per_limit"`           // 每人限购数量
	Status       int32 `gorm:"type:tinyint;not null;default:0" json:"status"` // 状态: 0=未开始, 1=进行中, 2=已结束
	CreatedAt    int64 `gorm:"column:created_at" json:"created_at"`           // Unix 时间戳
	UpdatedAt    int64 `gorm:"column:updated_at" json:"updated_at"`           // Unix 时间戳
}

// TableName 指定表名
func (SeckillProduct) TableName() string {
	return "seckill_products"
}
