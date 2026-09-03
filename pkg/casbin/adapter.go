package casbin

import (
	"errors"
	"fmt"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// casbinRuleTable 策略持久化表名，与 public/migrations/init_schema.sql 中 sys_casbin_rule DDL 对齐。
const casbinRuleTable = "sys_casbin_rule"

// CasbinRule 对应 sys_casbin_rule 表的行结构（casbin 标准策略表）。
type CasbinRule struct {
	ID    uint   `gorm:"primaryKey;autoIncrement"`
	Ptype string `gorm:"size:100"`
	V0    string `gorm:"size:100"`
	V1    string `gorm:"size:100"`
	V2    string `gorm:"size:100"`
	V3    string `gorm:"size:100"`
	V4    string `gorm:"size:100"`
	V5    string `gorm:"size:100"`
}

// TableName 指定使用的持久化表。
func (CasbinRule) TableName() string {
	return casbinRuleTable
}

// Adapter 是 casbin persist.Adapter 的自定义实现，
// 复用项目已有的 *gorm.DB（postgres），把策略持久化到 sys_casbin_rule 表。
// 替代 github.com/casbin/gorm-adapter/v3，避免其 blank import 多驱动带来的无用依赖链。
type Adapter struct {
	db *gorm.DB
}

var (
	_ persist.Adapter = (*Adapter)(nil)
)

// NewAdapter 基于已有的 *gorm.DB 创建策略适配器，表名为 sys_casbin_rule。
// 若表不存在则按 CasbinRule 结构自动建表（含 ptype+v0..v5 唯一索引），
// 与原有 gorm-adapter 的自动建表约定保持一致；生产 postgres 建表仍由迁移脚本
// public/migrations/init_schema.sql 负责。
func NewAdapter(db *gorm.DB) (*Adapter, error) {
	a := &Adapter{db: db}
	if err := a.createTable(); err != nil {
		return nil, err
	}
	return a, nil
}

// createTable 确保策略表存在：AutoMigrate 建表，并补建 ptype+v0..v5 唯一索引。
func (a *Adapter) createTable() error {
	rule := &CasbinRule{}
	if err := a.db.AutoMigrate(rule); err != nil {
		return err
	}
	indexName := "idx_" + casbinRuleTable
	hasIndex := a.db.Migrator().HasIndex(rule, indexName)
	if !hasIndex {
		if err := a.db.Exec(fmt.Sprintf(
			"CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (ptype,v0,v1,v2,v3,v4,v5)",
			indexName, casbinRuleTable,
		)).Error; err != nil {
			return err
		}
	}
	return nil
}

// ruleToPolicyArray 把一行策略转成 policy 数组，去掉尾部空字段。
// 例如 p, sub, obj, act, code 长度为 5；g, user_id, role_code 长度为 3。
func (a *Adapter) ruleToPolicyArray(line CasbinRule) []string {
	p := []string{line.Ptype, line.V0, line.V1, line.V2, line.V3, line.V4, line.V5}
	idx := len(p) - 1
	for idx >= 0 && p[idx] == "" {
		idx--
	}
	return p[:idx+1]
}

// savePolicyLine 把 ptype + rule 组装成一行，缺失字段留空。
func (a *Adapter) savePolicyLine(ptype string, rule []string) CasbinRule {
	line := CasbinRule{Ptype: ptype}
	for i, v := range rule {
		switch i {
		case 0:
			line.V0 = v
		case 1:
			line.V1 = v
		case 2:
			line.V2 = v
		case 3:
			line.V3 = v
		case 4:
			line.V4 = v
		case 5:
			line.V5 = v
		}
	}
	return line
}

// addRule 插入一行，冲突（ptype+v0..v5 重复）时忽略。
func (a *Adapter) addRule(line CasbinRule) error {
	return a.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error
}

// deleteRuleRaw 按非空字段构造 WHERE 删除。
// 注意不能使用主键删除：多行可能完全一样（不启用过滤场景下几乎不会），
// 且 casbin 删除语义按整行匹配。沿用 gorm-adapter 的做法，用 ptype+非空 v 字段精确过滤。
func (a *Adapter) deleteRuleRaw(line CasbinRule) error {
	queryStr := "ptype = ?"
	queryArgs := []interface{}{line.Ptype}
	if line.V0 != "" {
		queryStr += " and v0 = ?"
		queryArgs = append(queryArgs, line.V0)
	}
	if line.V1 != "" {
		queryStr += " and v1 = ?"
		queryArgs = append(queryArgs, line.V1)
	}
	if line.V2 != "" {
		queryStr += " and v2 = ?"
		queryArgs = append(queryArgs, line.V2)
	}
	if line.V3 != "" {
		queryStr += " and v3 = ?"
		queryArgs = append(queryArgs, line.V3)
	}
	if line.V4 != "" {
		queryStr += " and v4 = ?"
		queryArgs = append(queryArgs, line.V4)
	}
	if line.V5 != "" {
		queryStr += " and v5 = ?"
		queryArgs = append(queryArgs, line.V5)
	}
	return a.db.Delete(&CasbinRule{}, append([]interface{}{queryStr}, queryArgs...)...).Error
}

// LoadPolicy 读取全表，把每行转成 model 的 policy 并写入内存模型。
// 顺序按 ID 保证与 gorm-adapter 一致（p/g/g2 的插入顺序稳定）。
func (a *Adapter) LoadPolicy(model model.Model) error {
	var lines []CasbinRule
	if err := a.db.Order("id").Find(&lines).Error; err != nil {
		return err
	}
	for _, line := range lines {
		if err := persist.LoadPolicyArray(a.ruleToPolicyArray(line), model); err != nil {
			return err
		}
	}
	return nil
}

// SavePolicy 清空表后把 model 中 p 和 g 的全部策略写入。
// 与 gorm-adapter 语义对齐：truncate 后批量写，冲突忽略。
func (a *Adapter) SavePolicy(model model.Model) error {
	tx := a.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// postgres 使用 truncate（会重置 ID），与 gorm-adapter 的 truncateTable 对齐。
	if err := tx.Exec(fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY", casbinRuleTable)).Error; err != nil {
		tx.Rollback()
		return err
	}

	var lines []CasbinRule
	for _, ast := range model["p"] {
		for _, rule := range ast.Policy {
			lines = append(lines, a.savePolicyLine(ast.Key, rule))
		}
	}
	for _, ast := range model["g"] {
		for _, rule := range ast.Policy {
			lines = append(lines, a.savePolicyLine(ast.Key, rule))
		}
	}

	if len(lines) > 0 {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&lines).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit().Error
}

// AddPolicy 向表插入一条策略。
func (a *Adapter) AddPolicy(sec string, ptype string, rule []string) error {
	return a.addRule(a.savePolicyLine(ptype, rule))
}

// RemovePolicy 从表删除一条策略（按整行匹配）。
func (a *Adapter) RemovePolicy(sec string, ptype string, rule []string) error {
	return a.deleteRuleRaw(a.savePolicyLine(ptype, rule))
}

// RemoveFilteredPolicy 按 ptype + 过滤字段（fieldIndex 起）删除策略。
// fieldIndex==-1 表示删除该 ptype 的全部策略；否则按 fieldValues 逐字段过滤。
func (a *Adapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	line := CasbinRule{Ptype: ptype}

	if fieldIndex == -1 {
		return a.deleteRuleRaw(line)
	}

	if !hasNonEmptyField(fieldValues) {
		return errors.New("the query field cannot all be empty string (\"\"), please check")
	}

	setField := func(idx int, v string) {
		switch idx {
		case 0:
			line.V0 = v
		case 1:
			line.V1 = v
		case 2:
			line.V2 = v
		case 3:
			line.V3 = v
		case 4:
			line.V4 = v
		case 5:
			line.V5 = v
		}
	}
	for i, v := range fieldValues {
		abs := fieldIndex + i
		if abs >= 0 && abs <= 5 {
			setField(abs, v)
		}
	}

	return a.deleteRuleRaw(line)
}

func hasNonEmptyField(fieldValues []string) bool {
	for _, f := range fieldValues {
		if f != "" {
			return true
		}
	}
	return false
}