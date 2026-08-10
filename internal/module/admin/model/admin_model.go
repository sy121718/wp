package adminmodel

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
)

const tableNameSysAdmin = "sys_admin"

const (
	AdminStatusActive   = 1 // 启用
	AdminStatusInactive = 2 // 禁用
	AdminStatusBanned   = 3 // 封禁
)

const allowModifyIsAdminKey string = "allow_modify_is_admin"

// JSONMap 可序列化的 JSON 扩展字段，实现 sql.Scanner 和 driver.Valuer。
type JSONMap map[string]any

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

func (j *JSONMap) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("JSONMap Scan: 类型不是 []byte")
	}
	return json.Unmarshal(bytes, j)
}

// AdminEntity 对应 sys_admin 表字段。
type AdminEntity struct {
	ID                uint64     `gorm:"column:id;primaryKey"`                                        // 用户ID（唯一）
	DeptID            uint64     `gorm:"column:dept_id;type:bigint unsigned;default:0"`               // 所属部门ID
	Username          string     `gorm:"column:username;type:varchar(50);uniqueIndex"`                // 登录账号用户名
	Password          string     `gorm:"column:password;type:varchar(100)"`                           // 加密密码
	Name              *string    `gorm:"column:name;type:varchar(50)"`                                // 用户姓名
	Avatar            *string    `gorm:"column:avatar;type:varchar(255)"`                             // 头像URL
	Email             *string    `gorm:"column:email;type:varchar(100);index"`                        // 邮箱
	Phone             *string    `gorm:"column:phone;type:varchar(20);index"`                         // 手机号
	Status            int        `gorm:"column:status;type:tinyint(4);default:1;index"`               // 状态：1启用 2禁用 3封禁
	IsAdmin           int        `gorm:"column:is_admin;type:tinyint(4);default:0"`                   // 是否管理员：0否 1是
	LoginFailureCount uint16     `gorm:"column:login_failure_count;type:smallint unsigned;default:0"` // 连续登录失败次数
	LockedUntilTime   *time.Time `gorm:"column:locked_until_time;type:datetime(3)"`                   // 封禁至
	Metadata          JSONMap    `gorm:"column:metadata;type:json"`                                   // 扩展元数据
	LastFailureTime   *time.Time `gorm:"column:last_failure_time;type:datetime(3)"`                   // 最后一次登录失败时间
	RegisterIP        *string    `gorm:"column:register_ip;type:varchar(50)"`                         // 注册IP地址
	RegisterLocation  *string    `gorm:"column:register_location;type:varchar(100)"`                  // 注册地理位置
	LastLoginIP       *string    `gorm:"column:last_login_ip;type:varchar(50)"`                       // 最后登录IP
	LastLoginLocation *string    `gorm:"column:last_login_location;type:varchar(100)"`                // 最后登录地理位置
	LastLoginISP      *string    `gorm:"column:last_login_isp;type:varchar(50)"`                      // 最后登录网络运营商
	LastLoginTime     *time.Time `gorm:"column:last_login_time;type:datetime(3)"`                     // 最后登录时间
	CreateBy          uint64     `gorm:"column:create_by;type:bigint unsigned"`                       // 创建人ID
	CreateTime        *time.Time `gorm:"column:create_time;type:datetime(3)"`                         // 创建时间
	UpdateBy          uint64     `gorm:"column:update_by;type:bigint unsigned"`                       // 更新人ID
	UpdateTime        *time.Time `gorm:"column:update_time;type:datetime(3)"`
	Remark            *string    `gorm:"column:remark;type:varchar(255)"` // 备注
}

// AdminModel 持有 gorm 连接，供 service 层调用。
type AdminModel struct {
	db *gorm.DB
}

// // NewAdminModel 从 database 组件获取 db 并创建 model。
// func NewAdminModel() (*AdminModel, error) {
// 	db, err := database.GetDB()
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &AdminModel{db: db}, nil
// }

// NewAdminModelDemo 外部传入 db
func NewAdminModel(db *gorm.DB) *AdminModel {
	return &AdminModel{db: db}
}

// DB 返回绑定当前实体的数据库入口，支持链式调用增删改查。
func (m *AdminModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AdminEntity{})
}

// GetByID 根据 ID 查询管理员，不存在返回 nil。
func (m *AdminModel) GetByID(ctx context.Context, id uint64) (*AdminEntity, error) {
	var entity AdminEntity
	err := m.DB(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// DeleteByIDs 批量删除管理员。
func (m *AdminModel) DeleteByIDs(ctx context.Context, ids []uint64) (deleted int64, err error) {
	result := m.DB(ctx).Where("id IN ?", ids).Delete(&AdminEntity{})
	return result.RowsAffected, result.Error
}

// TableName 指定表名。
func (AdminEntity) TableName() string {
	return tableNameSysAdmin
}

// CanLogin 是否可以登录。
func (a *AdminEntity) CanLogin() bool {
	return a.Status == AdminStatusActive && !a.IsLocked()
}

// IsLocked 是否被锁定。
func (a *AdminEntity) IsLocked() bool {
	if a.LockedUntilTime == nil {
		return false
	}
	return time.Now().Before(*a.LockedUntilTime)
}

// IsActive 是否启用。
func (a *AdminEntity) IsActive() bool {
	return a.Status == AdminStatusActive
}

// IsSuperAdmin 是否超级管理员。
func (a *AdminEntity) IsSuperAdmin() bool {
	return a.IsAdmin == 1
}

// CanModifyField 是否允许修改字段（受保护字段默认不可外部修改）。
func (a *AdminEntity) CanModifyField(field string) bool {
	protectedFields := map[string]bool{
		"IsAdmin": true,
	}
	return !protectedFields[field]
}

// BeforeCreate 创建前 hook：补齐时间，并校验超级管理员唯一性。
func (a *AdminEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	a.CreateTime = &now
	a.UpdateTime = &now

	if a.IsAdmin == 1 {
		var count int64
		if err := tx.Model(&AdminEntity{}).Where("is_admin = 1").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("系统已存在超级管理员，不能重复创建")
		}
	}
	return nil
}

// BeforeUpdate 更新前 hook：更新时间，并阻止外部直接改 IsAdmin。
func (a *AdminEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	a.UpdateTime = &now

	if tx.Statement.Changed("IsAdmin") {
		if !allowModifyIsAdmin(tx.Statement.Context) {
			return errors.New("不允许外部修改")
		}
	}
	return nil
}

func allowModifyIsAdmin(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value := ctx.Value(allowModifyIsAdminKey)
	allowed, ok := value.(bool)
	return ok && allowed
}
