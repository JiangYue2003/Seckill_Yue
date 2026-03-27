package entity

// StockLog 库存流水
type StockLog struct {
	ID          int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID   int64  `gorm:"not null;index" json:"product_id"`                      // 商品ID
	OrderID     string `gorm:"type:varchar(32);not null;uniqueIndex" json:"order_id"` // 订单号
	ChangeType  int    `gorm:"not null" json:"change_type"`                           // 变更类型: 1=扣减, 2=回滚
	Quantity    int    `gorm:"not null" json:"quantity"`                              // 变更数量
	BeforeStock int    `gorm:"not null" json:"before_stock"`                          // 变更前库存
	AfterStock  int    `gorm:"not null" json:"after_stock"`                           // 变更后库存
	CreatedAt   int64  `gorm:"column:created_at;index" json:"created_at"`             // 创建时间戳
}

// TableName 指定表名
func (StockLog) TableName() string {
	return "stock_logs"
}

// 变更类型常量
const (
	StockChangeTypeDeduct   = 1 // 扣减
	StockChangeTypeRollback = 2 // 回滚
)
