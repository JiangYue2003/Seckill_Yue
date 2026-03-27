package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"seckill-mall/user-service/internal/config"
	"seckill-mall/user-service/internal/model/entity"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// UserModel 接口定义
type UserModel interface {
	// FindOneById 根据ID查询用户
	FindOneById(ctx context.Context, id int64) (*entity.User, error)

	// FindOneByUsername 根据用户名查询用户
	FindOneByUsername(ctx context.Context, username string) (*entity.User, error)

	// FindOneByEmail 根据邮箱查询用户
	FindOneByEmail(ctx context.Context, email string) (*entity.User, error)

	// Insert 创建用户
	Insert(ctx context.Context, user *entity.User) error

	// Update 更新用户
	Update(ctx context.Context, user *entity.User) error

	// UpdatePassword 更新密码
	UpdatePassword(ctx context.Context, id int64, password string) error

	// UpdateStatus 更新用户状态
	UpdateStatus(ctx context.Context, id int64, status int32) error
}

// NewUserModel 创建 UserModel 实例
func NewUserModel(c config.Config) (UserModel, error) {
	// 初始化 GORM 连接
	db, err := gorm.Open(mysql.Open(c.MySQL.DataSource), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 设置连接池参数
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return &userModel{
		db: db,
	}, nil
}

// userModel 实现 UserModel 接口
type userModel struct {
	db *gorm.DB
}

// FindOneById 根据ID查询用户
func (m *userModel) FindOneById(ctx context.Context, id int64) (*entity.User, error) {
	var user entity.User
	result := m.db.WithContext(ctx).Where("id = ?", id).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &user, nil
}

// FindOneByUsername 根据用户名查询用户
func (m *userModel) FindOneByUsername(ctx context.Context, username string) (*entity.User, error) {
	var user entity.User
	result := m.db.WithContext(ctx).Where("username = ?", username).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &user, nil
}

// FindOneByEmail 根据邮箱查询用户
func (m *userModel) FindOneByEmail(ctx context.Context, email string) (*entity.User, error) {
	var user entity.User
	result := m.db.WithContext(ctx).Where("email = ?", email).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &user, nil
}

// Insert 创建用户
func (m *userModel) Insert(ctx context.Context, user *entity.User) error {
	result := m.db.WithContext(ctx).Create(user)
	return result.Error
}

// Update 更新用户
func (m *userModel) Update(ctx context.Context, user *entity.User) error {
	result := m.db.WithContext(ctx).Save(user)
	return result.Error
}

// UpdatePassword 更新密码
func (m *userModel) UpdatePassword(ctx context.Context, id int64, password string) error {
	result := m.db.WithContext(ctx).Model(&entity.User{}).
		Where("id = ?", id).
		Update("password", password)
	return result.Error
}

// UpdateStatus 更新用户状态
func (m *userModel) UpdateStatus(ctx context.Context, id int64, status int32) error {
	result := m.db.WithContext(ctx).Model(&entity.User{}).
		Where("id = ?", id).
		Update("status", status)
	return result.Error
}
