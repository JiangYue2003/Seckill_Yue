package entity

// User 用户实体
// 使用 Unix 时间戳存储
type User struct {
	ID        int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Username  string `gorm:"type:varchar(50);not null;uniqueIndex" json:"username"`
	Password  string `gorm:"type:varchar(255);not null" json:"-"`
	Email     string `gorm:"type:varchar(100);not null;uniqueIndex" json:"email"`
	Phone     string `gorm:"type:varchar(20)" json:"phone"`
	Status    int32  `gorm:"type:tinyint;not null;default:1" json:"status"`
	CreatedAt int64  `gorm:"column:created_at" json:"created_at"` // Unix 时间戳
	UpdatedAt int64  `gorm:"column:updated_at" json:"updated_at"` // Unix 时间戳
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}
