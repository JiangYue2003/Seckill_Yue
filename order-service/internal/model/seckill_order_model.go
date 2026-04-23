package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"seckill-mall/order-service/internal/config"
	"seckill-mall/order-service/internal/model/entity"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SeckillOrderModel 秒杀订单记录 Model 接口
type SeckillOrderModel interface {
	// Insert 创建秒杀订单记录
	Insert(ctx context.Context, record *entity.SeckillOrder) error

	// BatchInsert 批量插入秒杀订单记录
	BatchInsert(ctx context.Context, records []*entity.SeckillOrder) error

	// FindByUserAndSeckillProduct 根据用户ID和秒杀商品ID查询
	FindByUserAndSeckillProduct(ctx context.Context, userId, seckillProductId int64) (*entity.SeckillOrder, error)

	// FindByOrderId 根据订单号查询
	FindByOrderId(ctx context.Context, orderId string) (*entity.SeckillOrder, error)
}

// NewSeckillOrderModel 创建 SeckillOrderModel 实例
func NewSeckillOrderModel(c config.Config) (SeckillOrderModel, error) {
	db, err := gorm.Open(mysql.Open(c.MySQL.DataSource), &gorm.Config{
		Logger: newGormLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}
	sqlDB.SetMaxIdleConns(50)  // 10 → 50，增加空闲连接
	sqlDB.SetMaxOpenConns(300) // 100 → 300，增加最大连接数
	sqlDB.SetConnMaxLifetime(time.Hour)

	return &seckillOrderModel{db: db}, nil
}

// seckillOrderModel 实现 SeckillOrderModel 接口
type seckillOrderModel struct {
	db *gorm.DB
}

// Insert 创建秒杀订单记录
func (m *seckillOrderModel) Insert(ctx context.Context, record *entity.SeckillOrder) error {
	result := m.db.WithContext(ctx).Create(record)
	return result.Error
}

// BatchInsert 批量插入秒杀订单记录（幂等）
// 使用 INSERT IGNORE，遇到唯一索引冲突（uk_user_seckill）自动跳过
func (m *seckillOrderModel) BatchInsert(ctx context.Context, records []*entity.SeckillOrder) error {
	if len(records) == 0 {
		return nil
	}

	// MySQL 使用 INSERT IGNORE
	result := m.db.WithContext(ctx).Clauses(clause.Insert{Modifier: "IGNORE"}).Create(records)
	return result.Error
}

// FindByUserAndSeckillProduct 根据用户ID和秒杀商品ID查询
func (m *seckillOrderModel) FindByUserAndSeckillProduct(ctx context.Context, userId, seckillProductId int64) (*entity.SeckillOrder, error) {
	var record entity.SeckillOrder
	result := m.db.WithContext(ctx).
		Where("user_id = ? AND seckill_product_id = ?", userId, seckillProductId).
		First(&record)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &record, nil
}

// FindByOrderId 根据订单号查询
func (m *seckillOrderModel) FindByOrderId(ctx context.Context, orderId string) (*entity.SeckillOrder, error) {
	var record entity.SeckillOrder
	result := m.db.WithContext(ctx).Where("order_id = ?", orderId).First(&record)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &record, nil
}
