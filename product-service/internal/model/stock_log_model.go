package model

import (
	"context"
	"fmt"
	"time"

	"seckill-mall/product-service/internal/config"
	"seckill-mall/product-service/internal/model/entity"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// StockLogModel 库存流水 Model 接口
type StockLogModel interface {
	// Insert 写入库存流水
	Insert(ctx context.Context, log *entity.StockLog) error

	// FindByOrderId 根据订单号查询
	FindByOrderId(ctx context.Context, orderId string) (*entity.StockLog, error)

	// FindByOrderIdAndChangeType 根据订单号和变更类型查询
	FindByOrderIdAndChangeType(ctx context.Context, orderId string, changeType int) (*entity.StockLog, error)
}

// NewStockLogModel 创建 StockLogModel 实例
func NewStockLogModel(c config.Config) (StockLogModel, error) {
	db, err := gorm.Open(mysql.Open(c.MySQL.DataSource), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return &stockLogModel{db: db}, nil
}

// stockLogModel 实现 StockLogModel 接口
type stockLogModel struct {
	db *gorm.DB
}

// Insert 写入库存流水
func (m *stockLogModel) Insert(ctx context.Context, log *entity.StockLog) error {
	return m.db.WithContext(ctx).Create(log).Error
}

// FindByOrderId 根据订单号查询
func (m *stockLogModel) FindByOrderId(ctx context.Context, orderId string) (*entity.StockLog, error) {
	var log entity.StockLog
	result := m.db.WithContext(ctx).Where("order_id = ?", orderId).First(&log)
	if result.Error != nil {
		return nil, result.Error
	}
	return &log, nil
}

// FindByOrderIdAndChangeType 根据订单号和变更类型查询
func (m *stockLogModel) FindByOrderIdAndChangeType(ctx context.Context, orderId string, changeType int) (*entity.StockLog, error) {
	var log entity.StockLog
	result := m.db.WithContext(ctx).
		Where("order_id = ? AND change_type = ?", orderId, changeType).
		First(&log)
	if result.Error != nil {
		return nil, result.Error
	}
	return &log, nil
}
