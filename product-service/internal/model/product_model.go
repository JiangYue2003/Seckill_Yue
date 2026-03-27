package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"seckill-mall/product-service/internal/config"
	"seckill-mall/product-service/internal/model/entity"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ProductModel 接口定义
type ProductModel interface {
	// FindOneById 根据ID查询商品
	FindOneById(ctx context.Context, id int64) (*entity.Product, error)

	// FindByKeyword 关键词搜索商品
	FindByKeyword(ctx context.Context, keyword string, status int32, page, pageSize int64) ([]*entity.Product, int64, error)

	// List 分页获取商品列表
	List(ctx context.Context, status int32, page, pageSize int64) ([]*entity.Product, int64, error)

	// Insert 创建商品
	Insert(ctx context.Context, product *entity.Product) error

	// Update 更新商品
	Update(ctx context.Context, product *entity.Product) error

	// Delete 删除商品
	Delete(ctx context.Context, id int64) error

	// DeductStock 扣减库存（乐观锁）
	DeductStock(ctx context.Context, id int64, quantity int, orderId string) (int, error)

	// RollbackStock 回滚库存
	RollbackStock(ctx context.Context, id int64, quantity int, orderId string) error

	// GetStock 获取库存
	GetStock(ctx context.Context, id int64) (int, error)
}

// SeckillProductModel 接口定义
type SeckillProductModel interface {
	// FindOneById 根据ID查询秒杀商品
	FindOneById(ctx context.Context, id int64) (*entity.SeckillProduct, error)

	// FindOneByProductId 根据商品ID查询秒杀商品
	FindOneByProductId(ctx context.Context, productId int64) (*entity.SeckillProduct, error)

	// ListActive 获取进行中的秒杀商品
	ListActive(ctx context.Context) ([]*entity.SeckillProduct, error)

	// Insert 创建秒杀商品
	Insert(ctx context.Context, seckillProduct *entity.SeckillProduct) error

	// Update 更新秒杀商品
	Update(ctx context.Context, seckillProduct *entity.SeckillProduct) error

	// DeductStock 扣减秒杀库存
	DeductStock(ctx context.Context, id int64, quantity int) error

	// UpdateSoldCount 更新已售数量
	UpdateSoldCount(ctx context.Context, id int64, soldCount int) error
}

// NewProductModel 创建 ProductModel 实例
func NewProductModel(c config.Config) (ProductModel, error) {
	db, err := gorm.Open(mysql.Open(c.MySQL.DataSource), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
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

	return &productModel{db: db}, nil
}

// NewSeckillProductModel 创建 SeckillProductModel 实例
func NewSeckillProductModel(c config.Config) (SeckillProductModel, error) {
	db, err := gorm.Open(mysql.Open(c.MySQL.DataSource), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
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

	return &seckillProductModel{db: db}, nil
}

// productModel 实现 ProductModel 接口
type productModel struct {
	db *gorm.DB
}

// seckillProductModel 实现 SeckillProductModel 接口
type seckillProductModel struct {
	db *gorm.DB
}

// FindOneById 根据ID查询商品
func (m *productModel) FindOneById(ctx context.Context, id int64) (*entity.Product, error) {
	var product entity.Product
	result := m.db.WithContext(ctx).Where("id = ?", id).First(&product)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &product, nil
}

// FindByKeyword 关键词搜索商品
func (m *productModel) FindByKeyword(ctx context.Context, keyword string, status int32, page, pageSize int64) ([]*entity.Product, int64, error) {
	var products []*entity.Product
	var total int64

	query := m.db.WithContext(ctx).Model(&entity.Product{})
	if keyword != "" {
		query = query.Where("name LIKE ? OR description LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if status > 0 {
		query = query.Where("status = ?", status)
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Offset(int(offset)).Limit(int(pageSize)).Find(&products).Error; err != nil {
		return nil, 0, err
	}

	return products, total, nil
}

// List 分页获取商品列表
func (m *productModel) List(ctx context.Context, status int32, page, pageSize int64) ([]*entity.Product, int64, error) {
	var products []*entity.Product
	var total int64

	query := m.db.WithContext(ctx).Model(&entity.Product{})
	if status > 0 {
		query = query.Where("status = ?", status)
	}

	// 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Offset(int(offset)).Limit(int(pageSize)).Find(&products).Error; err != nil {
		return nil, 0, err
	}

	return products, total, nil
}

// Insert 创建商品
func (m *productModel) Insert(ctx context.Context, product *entity.Product) error {
	return m.db.WithContext(ctx).Create(product).Error
}

// Update 更新商品
func (m *productModel) Update(ctx context.Context, product *entity.Product) error {
	return m.db.WithContext(ctx).Save(product).Error
}

// Delete 删除商品
func (m *productModel) Delete(ctx context.Context, id int64) error {
	return m.db.WithContext(ctx).Delete(&entity.Product{}, id).Error
}

// DeductStock 扣减库存（乐观锁）
func (m *productModel) DeductStock(ctx context.Context, id int64, quantity int, orderId string) (int, error) {
	// 使用乐观锁扣减库存
	// UPDATE products SET stock = stock - quantity, sold_count = sold_count + quantity
	// WHERE id = ? AND stock >= quantity
	result := m.db.WithContext(ctx).Model(&entity.Product{}).
		Where("id = ? AND stock >= ?", id, quantity).
		Updates(map[string]interface{}{
			"stock":      gorm.Expr("stock - ?", quantity),
			"sold_count": gorm.Expr("sold_count + ?", quantity),
		})

	if result.Error != nil {
		return 0, result.Error
	}

	if result.RowsAffected == 0 {
		return 0, ErrStockNotEnough
	}

	// 获取更新后的库存
	var product entity.Product
	if err := m.db.WithContext(ctx).Where("id = ?", id).First(&product).Error; err != nil {
		return 0, err
	}

	return product.Stock, nil
}

// RollbackStock 回滚库存
func (m *productModel) RollbackStock(ctx context.Context, id int64, quantity int, orderId string) error {
	result := m.db.WithContext(ctx).Model(&entity.Product{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"stock":      gorm.Expr("stock + ?", quantity),
			"sold_count": gorm.Expr("sold_count - ?", quantity),
		})

	return result.Error
}

// GetStock 获取库存
func (m *productModel) GetStock(ctx context.Context, id int64) (int, error) {
	var product entity.Product
	if err := m.db.WithContext(ctx).Where("id = ?", id).First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return product.Stock, nil
}

// FindOneById 根据ID查询秒杀商品
func (m *seckillProductModel) FindOneById(ctx context.Context, id int64) (*entity.SeckillProduct, error) {
	var seckillProduct entity.SeckillProduct
	result := m.db.WithContext(ctx).Where("id = ?", id).First(&seckillProduct)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &seckillProduct, nil
}

// FindOneByProductId 根据商品ID查询秒杀商品
func (m *seckillProductModel) FindOneByProductId(ctx context.Context, productId int64) (*entity.SeckillProduct, error) {
	var seckillProduct entity.SeckillProduct
	result := m.db.WithContext(ctx).Where("product_id = ?", productId).First(&seckillProduct)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &seckillProduct, nil
}

// ListActive 获取进行中的秒杀商品
func (m *seckillProductModel) ListActive(ctx context.Context) ([]*entity.SeckillProduct, error) {
	now := time.Now().Unix()
	var seckillProducts []*entity.SeckillProduct

	if err := m.db.WithContext(ctx).
		Where("status = ? AND start_time <= ? AND end_time >= ?", 1, now, now).
		Find(&seckillProducts).Error; err != nil {
		return nil, err
	}

	return seckillProducts, nil
}

// Insert 创建秒杀商品
func (m *seckillProductModel) Insert(ctx context.Context, seckillProduct *entity.SeckillProduct) error {
	return m.db.WithContext(ctx).Create(seckillProduct).Error
}

// Update 更新秒杀商品
func (m *seckillProductModel) Update(ctx context.Context, seckillProduct *entity.SeckillProduct) error {
	return m.db.WithContext(ctx).Save(seckillProduct).Error
}

// DeductStock 扣减秒杀库存
func (m *seckillProductModel) DeductStock(ctx context.Context, id int64, quantity int) error {
	result := m.db.WithContext(ctx).Model(&entity.SeckillProduct{}).
		Where("id = ? AND seckill_stock >= ?", id, quantity).
		Updates(map[string]interface{}{
			"seckill_stock": gorm.Expr("seckill_stock - ?", quantity),
			"sold_count":    gorm.Expr("sold_count + ?", quantity),
		})

	if result.RowsAffected == 0 {
		return ErrStockNotEnough
	}

	return result.Error
}

// UpdateSoldCount 更新已售数量
func (m *seckillProductModel) UpdateSoldCount(ctx context.Context, id int64, soldCount int) error {
	return m.db.WithContext(ctx).Model(&entity.SeckillProduct{}).
		Where("id = ?", id).
		Update("sold_count", soldCount).Error
}
