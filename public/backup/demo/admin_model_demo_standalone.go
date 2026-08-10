package demo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
)

// AdminEntityDemo 是“表结构映射”示例。
// 这个结构体专门对应数据库里的 sys_admin 表。
type AdminEntityDemo struct {
	ID                uint64     `gorm:"column:id;primaryKey"`
	Username          string     `gorm:"column:username;type:varchar(50)"`
	Password          string     `gorm:"column:password;type:varchar(100)"`
	Name              *string    `gorm:"column:name;type:varchar(50)"`
	Avatar            *string    `gorm:"column:avatar;type:varchar(255)"`
	Email             *string    `gorm:"column:email;type:varchar(100)"`
	Phone             *string    `gorm:"column:phone;type:varchar(20)"`
	Status            int8       `gorm:"column:status;type:tinyint(4)"`
	IsAdmin           int8       `gorm:"column:is_admin;type:tinyint(4)"`
	LoginFailureCount uint16     `gorm:"column:login_failure_count;type:smallint unsigned"`
	LockedUntilTime   *time.Time `gorm:"column:locked_until_time;type:datetime(3)"`
	LastFailureTime   *time.Time `gorm:"column:last_failure_time;type:datetime(3)"`
	RegisterIP        *string    `gorm:"column:register_ip;type:varchar(50)"`
	RegisterLocation  *string    `gorm:"column:register_location;type:varchar(100)"`
	LastLoginIP       *string    `gorm:"column:last_login_ip;type:varchar(50)"`
	LastLoginLocation *string    `gorm:"column:last_login_location;type:varchar(100)"`
	LastLoginISP      *string    `gorm:"column:last_login_isp;type:varchar(50)"`
	LastLoginTime     *time.Time `gorm:"column:last_login_time;type:datetime(3)"`
	CreateBy          uint64     `gorm:"column:create_by;type:bigint unsigned"`
	CreateTime        *time.Time `gorm:"column:create_time;type:datetime(3)"`
	UpdateBy          uint64     `gorm:"column:update_by;type:bigint unsigned"`
	UpdateTime        *time.Time `gorm:"column:update_time;type:datetime(3)"`
	DeletedTime       *time.Time `gorm:"column:deleted_time;type:datetime(3)"`
}

// TableName 指定这个结构体映射到哪张表。
func (AdminEntityDemo) TableName() string {
	return "sys_admin"
}

// AdminModelDemo 是“数据库操作层”示例。
// 它不代表表，只是把数据库查询方法组织在一起。
type AdminModelDemo struct {
	db *gorm.DB
}

// NewAdminModelDemo 外部传入 db（依赖注入）。
func NewAdminModelDemo(db *gorm.DB) *AdminModelDemo {
	return &AdminModelDemo{db: db}
}

// NewAdminModelDemoFromComponent 内部从项目数据库组件获取 db。
// 这样业务层不需要手动传 db，直接拿 model 即可。
func NewAdminModelDemoFromComponent() (*AdminModelDemo, error) {
	db, err := database.GetDB()
	if err != nil {
		return nil, err
	}
	return &AdminModelDemo{db: db}, nil
}

// baseQuery 统一返回基础查询对象：
// 1. 绑定 context（方便超时/链路追踪）
// 2. 指定查询模型是 AdminEntityDemo（对应 sys_admin）
// 3. 默认过滤软删除数据（deleted_time IS NULL）
func (m *AdminModelDemo) baseQuery(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AdminEntityDemo{}).Where("deleted_time IS NULL")
}

// GetByID 按 ID 查询管理员。
func (m *AdminModelDemo) GetByID(ctx context.Context, id uint64) (*AdminEntityDemo, error) {
	var admin AdminEntityDemo
	err := m.baseQuery(ctx).Where("id = ?", id).First(&admin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

// GetByUsername 按用户名查询管理员。
func (m *AdminModelDemo) GetByUsername(ctx context.Context, username string) (*AdminEntityDemo, error) {
	if username == "" {
		return nil, fmt.Errorf("username 不能为空")
	}

	var admin AdminEntityDemo
	err := m.baseQuery(ctx).Where("username = ?", username).First(&admin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &admin, nil
}
