package adminmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameSysRule = "sys_rule"

const tableNameSysRuleAssignment = "sys_rule_assignment"

const (
	RuleStatusDisabled = 0

	RuleStatusEnabled = 1
)

const (
	AssignmentTargetTypeRole = 1

	AssignmentTargetTypeUser = 2

	AssignmentTargetTypeDept = 3
)

const (
	AssignmentTargetScopeNone = 0

	AssignmentTargetScopeSelf = 1

	AssignmentTargetScopeSelfAndChildren = 2
)

type SysRuleEntity struct {
	ID       uint64 `gorm:"column:id;primaryKey"`
	RuleName string `gorm:"column:rule_name"`
	Domain   string `gorm:"column:domain;index"`
	Config   string `gorm:"column:config"`

	Status     int        `gorm:"column:status;index"`
	Remark     *string    `gorm:"column:remark"`
	CreateBy   uint64     `gorm:"column:create_by"`
	CreateTime *time.Time `gorm:"column:create_time"`
	UpdateBy   uint64     `gorm:"column:update_by"`
	UpdateTime *time.Time `gorm:"column:update_time"`
}

func (SysRuleEntity) TableName() string {
	return tableNameSysRule
}

func (e *SysRuleEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	e.UpdateTime = &now
	return nil
}

func (e *SysRuleEntity) BeforeUpdate(tx *gorm.DB) error {
	now := time.Now()
	e.UpdateTime = &now
	return nil
}

type SysRuleAssignmentEntity struct {
	ID          uint64     `gorm:"column:id;primaryKey"`
	RuleID      uint64     `gorm:"column:rule_id;index"`
	TargetType  int        `gorm:"column:target_type;index"`
	TargetID    uint64     `gorm:"column:target_id;index"`
	TargetScope int        `gorm:"column:target_scope;default:0"`
	CreateBy    uint64     `gorm:"column:create_by"`
	CreateTime  *time.Time `gorm:"column:create_time"`
}

func (SysRuleAssignmentEntity) TableName() string {
	return tableNameSysRuleAssignment
}

func (e *SysRuleAssignmentEntity) BeforeCreate(tx *gorm.DB) error {
	now := time.Now()
	e.CreateTime = &now
	return nil
}

type SysRuleModel struct {
	db *gorm.DB
}

func NewSysRuleModel(db *gorm.DB) *SysRuleModel {
	return &SysRuleModel{db: db}
}

func (m *SysRuleModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SysRuleEntity{})
}

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

func (m *SysRuleModel) Create(ctx context.Context, e *SysRuleEntity) error {
	return m.DB(ctx).Create(e).Error
}

func (m *SysRuleModel) Update(ctx context.Context, e *SysRuleEntity) error {
	return m.DB(ctx).
		Where("id = ?", e.ID).
		Select("rule_name", "domain", "config", "status", "remark", "update_by", "update_time").
		Updates(e).Error
}

func (m *SysRuleModel) DeleteByIDs(ctx context.Context, ids []uint64) (int64, error) {
	result := m.DB(ctx).Where("id IN ?", ids).Delete(&SysRuleEntity{})
	return result.RowsAffected, result.Error
}

func (m *SysRuleModel) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *SysRuleModel) LockByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) ([]SysRuleEntity, error) {
	list := make([]SysRuleEntity, 0, len(ids))
	if len(ids) == 0 {
		return list, nil
	}
	err := tx.WithContext(ctx).Model(&SysRuleEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).Order("id ASC").Find(&list).Error
	return list, err
}

func (m *SysRuleModel) DeleteByIDsTx(ctx context.Context, tx *gorm.DB, ids []uint64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := tx.WithContext(ctx).Model(&SysRuleEntity{}).Where("id IN ?", ids).Delete(&SysRuleEntity{})
	return result.RowsAffected, result.Error
}

type SysRuleAssignmentModel struct {
	db *gorm.DB
}

func NewSysRuleAssignmentModel(db *gorm.DB) *SysRuleAssignmentModel {
	return &SysRuleAssignmentModel{db: db}
}

func (m *SysRuleAssignmentModel) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SysRuleAssignmentEntity{})
}

func (m *SysRuleAssignmentModel) ListByRuleID(ctx context.Context, ruleID uint64) ([]SysRuleAssignmentEntity, error) {
	var list []SysRuleAssignmentEntity
	err := m.DB(ctx).Where("rule_id = ?", ruleID).Order("id ASC").Find(&list).Error
	return list, err
}

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

func (m *SysRuleAssignmentModel) DeleteByRuleID(ctx context.Context, ruleID uint64) error {
	return m.DB(ctx).Where("rule_id = ?", ruleID).Delete(&SysRuleAssignmentEntity{}).Error
}

func (m *SysRuleAssignmentModel) DeleteByRuleIDTx(ctx context.Context, tx *gorm.DB, ruleID uint64) error {
	return tx.WithContext(ctx).Model(&SysRuleAssignmentEntity{}).
		Where("rule_id = ?", ruleID).Delete(&SysRuleAssignmentEntity{}).Error
}

func (m *SysRuleAssignmentModel) BatchCreate(ctx context.Context, entities []SysRuleAssignmentEntity) error {
	if len(entities) == 0 {
		return nil
	}
	return m.DB(ctx).Create(&entities).Error
}
