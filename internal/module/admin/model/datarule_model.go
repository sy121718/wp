// Package adminmodel 合并后的统一模型包。
package adminmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// 表常量：规则主表
const tableNameSysRule = "sys_rule"

// 表常量：规则分配表
const tableNameSysRuleAssignment = "sys_rule_assignment"

const (
	// RuleStatusDisabled 规则禁用状态
	RuleStatusDisabled = 0
	// RuleStatusEnabled 规则启用状态
	RuleStatusEnabled = 1
)

const (
	// AssignmentTargetTypeRole 分配目标类型：角色
	AssignmentTargetTypeRole = 1
	// AssignmentTargetTypeUser 分配目标类型：用户
	AssignmentTargetTypeUser = 2
	// AssignmentTargetTypeDept 分配目标类型：部门
	AssignmentTargetTypeDept = 3
)

const (
	// AssignmentTargetScopeNone 角色和用户分配不使用范围。
	AssignmentTargetScopeNone = 0
	// AssignmentTargetScopeSelf 规则仅作用于当前部门。
	AssignmentTargetScopeSelf = 1
	// AssignmentTargetScopeSelfAndChildren 规则作用于当前部门及全部下级部门。
	AssignmentTargetScopeSelfAndChildren = 2
)

// SysRuleEntity 对应 sys_rule 表。
type SysRuleEntity struct {
	ID       uint64 `gorm:"column:id;primaryKey"`
	RuleName string `gorm:"column:rule_name;type:varchar(100)"`
	Domain   string `gorm:"column:domain;type:varchar(50);index"`
	Config   string `gorm:"column:config;type:json"`
	// Status 不带 gorm default tag：gorm 对带 default 的字段在零值时会用 DB 默认值
	// 替换并回写 struct，导致显式传入的 status=0（禁用）被改写成 1（启用）。
	// service 层总是显式设置 Status（RuleCreate/RuleUpdate），DB 列 DEFAULT 1 仅兜底直接 SQL 插入。
	Status     int        `gorm:"column:status;type:tinyint;index"`
	Remark     *string    `gorm:"column:remark;type:varchar(200)"`
	CreateBy   uint64     `gorm:"column:create_by;type:bigint unsigned"`
	CreateTime *time.Time `gorm:"column:create_time;type:datetime(3)"`
	UpdateBy   uint64     `gorm:"column:update_by;type:bigint unsigned"`
	UpdateTime *time.Time `gorm:"column:update_time;type:datetime(3)"`
}

// TableName 返回 sys_rule 表名。
func (SysRuleEntity) TableName() string {
	return tableNameSysRule
}

// BeforeCreate GORM 钩子：写入前自动设置创建时间和更新时间。
func (e *SysRuleEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

// BeforeUpdate GORM 钩子：更新前自动设置更新时间。
func (e *SysRuleEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

// SysRuleAssignmentEntity 对应 sys_rule_assignment 表。
type SysRuleAssignmentEntity struct {
	ID          uint64     `gorm:"column:id;primaryKey"`
	RuleID      uint64     `gorm:"column:rule_id;type:bigint unsigned;index"`
	TargetType  int        `gorm:"column:target_type;type:tinyint;index"`
	TargetID    uint64     `gorm:"column:target_id;type:bigint unsigned;index"`
	TargetScope int        `gorm:"column:target_scope;type:tinyint;default:0"`
	CreateBy    uint64     `gorm:"column:create_by;type:bigint unsigned"`
	CreateTime  *time.Time `gorm:"column:create_time;type:datetime(3)"`
}

// TableName 返回 sys_rule_assignment 表名。
func (SysRuleAssignmentEntity) TableName() string {
	return tableNameSysRuleAssignment
}

// BeforeCreate GORM 钩子：写入前自动设置创建时间。
func (e *SysRuleAssignmentEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	return nil
}

// SysRuleModel 数据规则表（sys_rule）数据访问。
type SysRuleModel struct {
	db *gorm.DB
}

// NewSysRuleModel 创建 SysRuleModel 实例。
func NewSysRuleModel(db *gorm.DB) *SysRuleModel {
	return &SysRuleModel{db: db}
}

// DB 返回绑定 sys_rule 表的查询上下文。
func (m *SysRuleModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SysRuleEntity{})
}

// GetByID 按主键查询单条规则，未找到返回 nil。
func (m *SysRuleModel) GetByID(ctx context.Context, id uint64) (*SysRuleEntity, error) {
	var entity SysRuleEntity
	err := m.DB(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// Create 插入一条规则记录。
func (m *SysRuleModel) Create(ctx context.Context, e *SysRuleEntity) error {
	return m.DB(ctx).Create(e).Error
}

// Update 按主键更新规则记录。
func (m *SysRuleModel) Update(ctx context.Context, e *SysRuleEntity) error {
	return m.DB(ctx).
		Where("id = ?", e.ID).
		Select("rule_name", "domain", "config", "status", "remark", "update_by", "update_time").
		Updates(e).Error
}

// DeleteByIDs 按主键批量删除规则，返回影响行数。
func (m *SysRuleModel) DeleteByIDs(ctx context.Context, ids []uint64) (int64, error) {
	result := m.DB(ctx).Where("id IN ?", ids).Delete(&SysRuleEntity{})
	return result.RowsAffected, result.Error
}

// SysRuleAssignmentModel 规则分配表（sys_rule_assignment）数据访问。
type SysRuleAssignmentModel struct {
	db *gorm.DB
}

// NewSysRuleAssignmentModel 创建 SysRuleAssignmentModel 实例。
func NewSysRuleAssignmentModel(db *gorm.DB) *SysRuleAssignmentModel {
	return &SysRuleAssignmentModel{db: db}
}

// DB 返回绑定 sys_rule_assignment 表的查询上下文。
func (m *SysRuleAssignmentModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SysRuleAssignmentEntity{})
}

// ListByRuleID 按规则 ID 查询所有分配记录，按主键升序排列。
func (m *SysRuleAssignmentModel) ListByRuleID(ctx context.Context, ruleID uint64) ([]SysRuleAssignmentEntity, error) {
	var list []SysRuleAssignmentEntity
	err := m.DB(ctx).Where("rule_id = ?", ruleID).Order("id ASC").Find(&list).Error
	return list, err
}

// ReplaceByRuleID 在同一事务中全量替换规则分配记录。
func (m *SysRuleAssignmentModel) ReplaceByRuleID(
	ctx context.Context,
	ruleID uint64,
	entities []SysRuleAssignmentEntity,
) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("rule_id = ?", ruleID).Delete(&SysRuleAssignmentEntity{}).Error; err != nil {
			return err
		}
		if len(entities) == 0 {
			return nil
		}
		return tx.Create(&entities).Error
	})
}

// DeleteByRuleID 按规则 ID 删除所有分配记录。
func (m *SysRuleAssignmentModel) DeleteByRuleID(ctx context.Context, ruleID uint64) error {
	return m.DB(ctx).Where("rule_id = ?", ruleID).Delete(&SysRuleAssignmentEntity{}).Error
}

// BatchCreate 批量插入分配记录。传入空切片时直接返回 nil。
func (m *SysRuleAssignmentModel) BatchCreate(ctx context.Context, entities []SysRuleAssignmentEntity) error {
	if len(entities) == 0 {
		return nil
	}
	return m.DB(ctx).Create(&entities).Error
}
