package adminmodel

import (
	"context"
	"database/sql/driver"
	"errors"
	json "go_wp/pkg/jsonx"
	"time"

	adminenums "go_wp/internal/module/admin/enums"

	"go_wp/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameSysAdmin = "sys_admin"

const (
	AdminStatusActive   = 1
	AdminStatusInactive = 2
	AdminStatusBanned   = 3
)

const allowModifyIsAdminKey string = "allow_modify_is_admin"

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

type AdminEntity struct {
	ID                uint64     `gorm:"column:id;primaryKey"`
	DeptID            uint64     `gorm:"column:dept_id;default:0" datarule:"label=所属部门;ops=EQ,NEQ,IN,NOT_IN"`
	Username          string     `gorm:"column:username;uniqueIndex" datarule:"label=用户名;ops=EQ,NEQ,LIKE,NOT_LIKE"`
	Password          string     `gorm:"column:password"`
	Name              *string    `gorm:"column:name"`
	Avatar            *string    `gorm:"column:avatar"`
	Email             *string    `gorm:"column:email;index" datarule:"label=邮箱;ops=EQ,NEQ,LIKE"`
	Phone             *string    `gorm:"column:phone;index" datarule:"label=手机号;ops=EQ,NEQ"`
	Status            int        `gorm:"column:status;default:1;index" datarule:"label=状态;ops=EQ,NEQ,IN,NOT_IN"`
	IsAdmin           int        `gorm:"column:is_admin;default:0"`
	LoginFailureCount uint16     `gorm:"column:login_failure_count;default:0"`
	LockedUntilTime   *time.Time `gorm:"column:locked_until_time"`
	Metadata          JSONMap    `gorm:"column:metadata;type:json"`
	LastFailureTime   *time.Time `gorm:"column:last_failure_time"`
	RegisterIP        *string    `gorm:"column:register_ip"`
	RegisterLocation  *string    `gorm:"column:register_location"`
	LastLoginIP       *string    `gorm:"column:last_login_ip"`
	LastLoginLocation *string    `gorm:"column:last_login_location"`
	LastLoginISP      *string    `gorm:"column:last_login_isp"`
	LastLoginTime     *time.Time `gorm:"column:last_login_time"`
	CreateBy          uint64     `gorm:"column:create_by"`
	CreateTime        *time.Time `gorm:"column:create_time"`
	UpdateBy          uint64     `gorm:"column:update_by"`
	UpdateTime        *time.Time `gorm:"column:update_time"`
	Remark            *string    `gorm:"column:remark"`
}

type AdminModel struct {
	db *gorm.DB
}

func NewAdminModel(db *gorm.DB) *AdminModel {
	return &AdminModel{db: db}
}

func (m *AdminModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&AdminEntity{})
}

func (m *AdminModel) DialectName() string {
	return m.db.Dialector.Name()
}

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

func (m *AdminModel) DeleteByIDs(ctx context.Context, ids []uint64) (deleted int64, err error) {
	result := m.DB(ctx).Where("id IN ?", ids).Delete(&AdminEntity{})
	return result.RowsAffected, result.Error
}

func (m *AdminModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// UpdateDeptIDByIDsTx 在调用方事务内把一批成员的部门改成 deptID。
//
// 归属调整要走 model：service 不碰 DB 句柄，也不在 service 里拼 SQL 形状。
func (m *AdminModel) UpdateDeptIDByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64, deptID uint64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := tx.WithContext(ctx).Model(&AdminEntity{}).
		Where("id IN ?", ids).
		Update("dept_id", deptID)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (m *AdminModel) LockByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) ([]AdminEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []AdminEntity
	if err := tx.WithContext(ctx).Model(&AdminEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).Order("id ASC").Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

func (m *AdminModel) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (deleted int64, err error) {
	result := tx.WithContext(ctx).Model(&AdminEntity{}).Where("id IN ?", ids).Delete(&AdminEntity{})
	return result.RowsAffected, result.Error
}

// CountByDeptIDTx 在调用方事务内数部门下的成员。
//
// 停用成员仍然属于该部门，所以按 dept_id 数、不按状态过滤。
// 删除部门前的这项校验必须读本事务：非事务版看不到本事务里刚改过 dept_id 的行。
func (m *AdminModel) CountByDeptIDTx(ctx context.Context, tx *gorm.DB, deptID uint64) (int64, error) {
	var count int64
	err := tx.WithContext(ctx).Model(&AdminEntity{}).Where("dept_id = ?", deptID).Count(&count).Error
	return count, err
}

func (m *AdminModel) IncrementLoginFailure(ctx context.Context, id uint64, lockThreshold int, lockDuration time.Duration) error {
	now := time.Now()
	lockedUntil := now.Add(lockDuration)
	return m.db.WithContext(ctx).Model(&AdminEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"login_failure_count": gorm.Expr("login_failure_count + 1"),
			"last_failure_time":   now,
			"locked_until_time":   gorm.Expr("CASE WHEN login_failure_count + 1 >= ? THEN ? ELSE locked_until_time END", lockThreshold, lockedUntil),
		}).Error
}

func (m *AdminModel) ResetLoginFailure(ctx context.Context, id uint64) error {
	return m.db.WithContext(ctx).Model(&AdminEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"login_failure_count": 0,
			"locked_until_time":   nil,
			"last_failure_time":   nil,
		}).Error
}

func (AdminEntity) TableName() string {
	return tableNameSysAdmin
}

func (a *AdminEntity) CanLogin() bool {
	return a.Status == AdminStatusActive && !a.IsLocked()
}

func (a *AdminEntity) IsLocked() bool {
	if a.LockedUntilTime == nil {
		return false
	}
	return time.Now().Before(*a.LockedUntilTime)
}

func (a *AdminEntity) IsActive() bool {
	return a.Status == AdminStatusActive
}

func (a *AdminEntity) IsSuperAdmin() bool {
	return a.IsAdmin == 1
}

func (a *AdminEntity) CanModifyField(field string) bool {
	protectedFields := map[string]bool{
		"IsAdmin": true,
	}
	return !protectedFields[field]
}

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
			return adminenums.Err(adminenums.ErrSuperAdminExists)
		}
	}
	return nil
}

func (a *AdminEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	a.UpdateTime = &now

	if tx.Statement.Changed("IsAdmin") {
		if !allowModifyIsAdmin(tx.Statement.Context) {
			return adminenums.Err(adminenums.ErrFieldProtected)
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

// AdminListFilter 是管理员列表的筛选条件。
//
// 筛选/计数/分页属于数据库行为，归 model：service 只把请求里的条件搬过来，
// 不再自己拼 Where/Count/Find。
type AdminListFilter struct {
	Keyword   string
	Email     string
	Name      string
	Status    *int
	SortField string
	SortOrder string
	Page      int
	Limit     int
}

func (f AdminListFilter) orderClause() string {
	if f.SortField == "" || f.SortOrder == "" {
		return "id DESC"
	}
	return f.SortField + " " + f.SortOrder
}

// ListPage 分页查管理员列表（排除超管），返回总数与本页行。
func (m *AdminModel) ListPage(ctx context.Context, f AdminListFilter) (int64, []AdminEntity, error) {
	query := m.DB(ctx).Where("is_admin != ?", 1)
	if f.Email != "" {
		query = query.Where("email LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(f.Email)+"%")
	}
	if f.Name != "" {
		query = query.Where("name LIKE ? ESCAPE '\\'", "%"+database.EscapeLikePattern(f.Name)+"%")
	}
	if f.Status != nil {
		query = query.Where("status = ?", *f.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, nil, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 1
	}
	page := f.Page
	if page <= 0 {
		page = 1
	}
	list := make([]AdminEntity, 0, limit)
	err := query.Order(f.orderClause()).
		Offset((page - 1) * limit).Limit(limit).Find(&list).Error
	return total, list, err
}

// DetailByID 读管理员详情列，返回该行（不存在时 ID 为 0）。
func (m *AdminModel) DetailByID(ctx context.Context, id uint64) (*AdminEntity, error) {
	var entity AdminEntity
	err := m.DB(ctx).
		Select("id", "username", "name", "avatar", "email", "phone", "status", "is_admin",
			"register_ip", "register_location", "last_login_ip", "last_login_location",
			"last_login_time", "create_by", "create_time", "remark").
		Where("id = ?", id).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// Create 新增管理员。
func (m *AdminModel) Create(ctx context.Context, e *AdminEntity) error {
	return m.DB(ctx).Create(e).Error
}

// UpdateByID 按主键更新管理员（只写实体上赋过值的列）。
func (m *AdminModel) UpdateByID(ctx context.Context, id uint64, e *AdminEntity) error {
	return m.DB(ctx).Where("id = ?", id).Updates(e).Error
}

// ListPageByDept 分页查某部门的成员，返回总数与本页行。
func (m *AdminModel) ListPageByDept(ctx context.Context, deptID uint64, page, limit int) (int64, []AdminEntity, error) {
	query := m.DB(ctx).Where("dept_id = ?", deptID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return 0, nil, err
	}

	if limit <= 0 {
		limit = 1
	}
	if page <= 0 {
		page = 1
	}
	var entities []AdminEntity
	err := query.Select("id, username, name, email, status").
		Offset((page - 1) * limit).Limit(limit).
		Order("id ASC").
		Find(&entities).Error
	return total, entities, err
}

// CountByDeptID 数某部门的成员（含停用：停用成员仍属这个部门）。
func (m *AdminModel) CountByDeptID(ctx context.Context, deptID uint64) (int64, error) {
	var count int64
	err := m.DB(ctx).Where("dept_id = ?", deptID).Count(&count).Error
	return count, err
}

// FindByLoginName 按登录名或邮箱查账号。
//
// 「登录名按大小写敏感比对」是数据库方言差异，所以在 model 里按方言选表达式；
// service 不再持 DB 句柄，也不判断方言。
func (m *AdminModel) FindByLoginName(ctx context.Context, login string) (*AdminEntity, error) {
	expr := "CAST(username AS BINARY) = CAST(? AS BINARY)"
	if m.DialectName() == "postgres" {
		expr = "username = ?"
	}
	var entity AdminEntity
	err := m.DB(ctx).Where(expr+" OR email = ?", login, login).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// TouchLastLogin 记录这次登录的时刻与来源 IP。
func (m *AdminModel) TouchLastLogin(ctx context.Context, id uint64, at time.Time, ip string) error {
	update := map[string]any{"last_login_time": at}
	if ip != "" {
		update["last_login_ip"] = ip
	}
	return m.DB(ctx).Where("id = ?", id).
		Select("last_login_time", "last_login_ip").
		Updates(update).Error
}

// FirstActiveSuperAdmin 取第一个启用的超管账号，用于本地开发登录。
func (m *AdminModel) FirstActiveSuperAdmin(ctx context.Context, username string) (*AdminEntity, error) {
	query := m.DB(ctx).Where("is_admin = ?", 1)
	if username != "" {
		query = query.Where("username = ?", username)
	}
	var entity AdminEntity
	err := query.Order("id ASC").First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// FieldExists 判断某列是否已被占用（新建时用，不带排除行）。
func (m *AdminModel) FieldExists(ctx context.Context, field string, value any) (bool, error) {
	var count int64
	err := m.DB(ctx).Where(field+" = ?", value).Count(&count).Error
	return count > 0, err
}

// FieldExistsTxExcept 在事务内判断某列是否被自己以外的行占用。
//
// 调用前必须已经锁住目标行：预检与写入之间若没有行锁，两个并发编辑会各自看到「没重名」。
func (m *AdminModel) FieldExistsTxExcept(ctx context.Context, tx *gorm.DB, field string, value any, excludeID uint64) (bool, error) {
	query := tx.WithContext(ctx).Model(&AdminEntity{}).Where(field+" = ?", value)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}

// UpdateByIDTx 在事务内更新一行。
func (m *AdminModel) UpdateByIDTx(ctx context.Context, tx *gorm.DB, id uint64, e *AdminEntity) error {
	return tx.WithContext(ctx).Model(&AdminEntity{}).Where("id = ?", id).Updates(e).Error
}
