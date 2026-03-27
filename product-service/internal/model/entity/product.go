package entity

// Product 商品实体
type Product struct {
	ID          int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string `gorm:"type:varchar(200);not null" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Price       int64  `gorm:"not null" json:"price"`                         // 价格（分）
	Stock       int    `gorm:"not null;default:0" json:"stock"`               // 库存数量
	SoldCount   int    `gorm:"not null;default:0" json:"sold_count"`          // 已售数量
	CoverImage  string `gorm:"type:varchar(500)" json:"cover_image"`          // 封面图URL
	Status      int32  `gorm:"type:tinyint;not null;default:1" json:"status"` // 状态: 1=上架, 0=下架
	CreatedAt   int64  `gorm:"column:created_at" json:"created_at"`           // Unix 时间戳
	UpdatedAt   int64  `gorm:"column:updated_at" json:"updated_at"`           // Unix 时间戳
}

// TableName 指定表名
func (Product) TableName() string {
	return "products"
}
