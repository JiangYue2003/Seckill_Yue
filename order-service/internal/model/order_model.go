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

// OrderModel 接口定义
type OrderModel interface {
	// FindOneByOrderId 根据订单号查询订单
	FindOneByOrderId(ctx context.Context, orderId string) (*entity.Order, error)

	// FindByUserId 分页获取用户订单
	FindByUserId(ctx context.Context, userId int64, status int32, page, pageSize int64) ([]*entity.Order, int64, error)

	// Insert 创建订单
	Insert(ctx context.Context, order *entity.Order) error

	// BatchInsert 批量插入订单（幂等）
	BatchInsert(ctx context.Context, orders []*entity.Order) (int64, error)

	// Update 更新订单
	Update(ctx context.Context, order *entity.Order) error

	// UpdateStatus 更新订单状态
	UpdateStatus(ctx context.Context, orderId string, status int32) error

	// Pay 支付订单
	Pay(ctx context.Context, orderId string, paymentId string) error

	// Cancel 取消订单
	Cancel(ctx context.Context, orderId string, userId int64) error

	// Refund 退款
	Refund(ctx context.Context, orderId string) error

	// CheckIdempotency 检查幂等性
	CheckIdempotency(ctx context.Context, orderId string) (bool, error)

	// BatchCheckIdempotency 批量检查幂等性
	// 返回 map[orderId]bool，true 表示已存在
	BatchCheckIdempotency(ctx context.Context, orderIds []string) (map[string]bool, error)
}

// NewOrderModel 创建 OrderModel 实例
func NewOrderModel(c config.Config) (OrderModel, error) {
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

	return &orderModel{db: db}, nil
}

// orderModel 实现 OrderModel 接口
type orderModel struct {
	db *gorm.DB
}

// FindOneByOrderId 根据订单号查询订单
func (m *orderModel) FindOneByOrderId(ctx context.Context, orderId string) (*entity.Order, error) {
	var order entity.Order
	result := m.db.WithContext(ctx).Where("order_id = ?", orderId).First(&order)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &order, nil
}

// FindByUserId 分页获取用户订单
func (m *orderModel) FindByUserId(ctx context.Context, userId int64, status int32, page, pageSize int64) ([]*entity.Order, int64, error) {
	var orders []*entity.Order
	var total int64

	query := m.db.WithContext(ctx).Model(&entity.Order{}).Where("user_id = ?", userId)
	if status > 0 {
		query = query.Where("status = ?", status)
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Offset(int(offset)).Limit(int(pageSize)).Order("created_at DESC").Find(&orders).Error; err != nil {
		return nil, 0, err
	}

	return orders, total, nil
}

// Insert 创建订单
func (m *orderModel) Insert(ctx context.Context, order *entity.Order) error {
	return m.db.WithContext(ctx).Create(order).Error
}

// BatchInsert 批量插入订单（幂等）
// 使用 INSERT IGNORE 语法，遇到重复 order_id 自动跳过
func (m *orderModel) BatchInsert(ctx context.Context, orders []*entity.Order) (int64, error) {
	if len(orders) == 0 {
		return 0, nil
	}

	// MySQL 使用 INSERT IGNORE，遇到主键冲突跳过
	result := m.db.WithContext(ctx).Clauses(clause.Insert{Modifier: "IGNORE"}).Create(orders)
	return result.RowsAffected, result.Error
}

// Update 更新订单
func (m *orderModel) Update(ctx context.Context, order *entity.Order) error {
	return m.db.WithContext(ctx).Save(order).Error
}

// UpdateStatus 更新订单状态
func (m *orderModel) UpdateStatus(ctx context.Context, orderId string, status int32) error {
	result := m.db.WithContext(ctx).Model(&entity.Order{}).
		Where("order_id = ?", orderId).
		Update("status", status)
	return result.Error
}

// Pay 支付订单
func (m *orderModel) Pay(ctx context.Context, orderId string, paymentId string) error {
	result := m.db.WithContext(ctx).Model(&entity.Order{}).
		Where("order_id = ? AND status = ?", orderId, entity.OrderStatusPending).
		Updates(map[string]interface{}{
			"status":     entity.OrderStatusPaid,
			"payment_id": paymentId,
			"paid_at":    time.Now().Unix(),
		})

	if result.RowsAffected == 0 {
		return ErrOrderCannotPay
	}

	return result.Error
}

// Cancel 取消订单
func (m *orderModel) Cancel(ctx context.Context, orderId string, userId int64) error {
	result := m.db.WithContext(ctx).Model(&entity.Order{}).
		Where("order_id = ? AND user_id = ? AND status = ?", orderId, userId, entity.OrderStatusPending).
		Update("status", entity.OrderStatusCancelled)

	if result.RowsAffected == 0 {
		return ErrOrderCannotCancel
	}

	return result.Error
}

// Refund 退款
func (m *orderModel) Refund(ctx context.Context, orderId string) error {
	result := m.db.WithContext(ctx).Model(&entity.Order{}).
		Where("order_id = ? AND status IN (?, ?)", orderId, entity.OrderStatusPaid, entity.OrderStatusCompleted).
		Update("status", entity.OrderStatusRefunded)

	if result.RowsAffected == 0 {
		return ErrOrderCannotRefund
	}

	return result.Error
}

// CheckIdempotency 检查幂等性
func (m *orderModel) CheckIdempotency(ctx context.Context, orderId string) (bool, error) {
	var count int64
	err := m.db.WithContext(ctx).Model(&entity.Order{}).
		Where("order_id = ?", orderId).
		Count(&count).Error

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// BatchCheckIdempotency 批量检查幂等性
// 返回 map[orderId]bool，true 表示已存在
func (m *orderModel) BatchCheckIdempotency(ctx context.Context, orderIds []string) (map[string]bool, error) {
	if len(orderIds) == 0 {
		return make(map[string]bool), nil
	}

	var existingOrders []entity.Order
	err := m.db.WithContext(ctx).
		Select("order_id").
		Where("order_id IN ?", orderIds).
		Find(&existingOrders).Error

	if err != nil {
		return nil, err
	}

	result := make(map[string]bool, len(existingOrders))
	for _, order := range existingOrders {
		result[order.OrderId] = true
	}
	return result, nil
}
