package entity

type SeckillStockShard struct {
	ID              int64 `gorm:"primaryKey;autoIncrement" json:"id"`
	SeckillProductID int64 `gorm:"column:seckill_product_id;not null;uniqueIndex:uk_spid_shard_no,priority:1;index:idx_shard_available,priority:1" json:"seckill_product_id"`
	ShardNo         int32 `gorm:"column:shard_no;not null;uniqueIndex:uk_spid_shard_no,priority:2" json:"shard_no"`
	Stock           int   `gorm:"column:stock;not null;default:0" json:"stock"`
	AvailableStock  int   `gorm:"column:available_stock;not null;default:0;index:idx_shard_available,priority:2" json:"available_stock"`
	SoldCount       int   `gorm:"column:sold_count;not null;default:0" json:"sold_count"`
	Version         int32 `gorm:"column:version;not null;default:0" json:"version"`
	CreatedAt       int64 `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       int64 `gorm:"column:updated_at" json:"updated_at"`
}

func (SeckillStockShard) TableName() string {
	return "seckill_stock_shards"
}
