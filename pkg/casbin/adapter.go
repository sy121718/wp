package casbin

import (
	"errors"
	"fmt"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const casbinRuleTable = "sys_casbin_rule"

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

func (CasbinRule) TableName() string {
	return casbinRuleTable
}

type Adapter struct {
	db *gorm.DB
}

var (
	_ persist.Adapter = (*Adapter)(nil)
)

func NewAdapter(db *gorm.DB) (*Adapter, error) {
	a := &Adapter{db: db}
	if err := a.createTable(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Adapter) createTable() error {
	rule := &CasbinRule{}
	var tableExists bool
	if err := a.db.Raw("SELECT to_regclass(?) IS NOT NULL", casbinRuleTable).Scan(&tableExists).Error; err != nil {
		return err
	}
	if !tableExists {
		if err := a.db.AutoMigrate(rule); err != nil {
			return err
		}
	}

	indexName := "idx_" + casbinRuleTable
	var indexExists bool
	if err := a.db.Raw(`SELECT EXISTS (
		SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'i' AND c.relname = ? AND n.nspname = current_schema())`,
		indexName).Scan(&indexExists).Error; err != nil {
		return err
	}
	if indexExists {
		return nil
	}
	return a.db.Exec(fmt.Sprintf(
		"CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (ptype,v0,v1,v2,v3,v4,v5)",
		indexName, casbinRuleTable,
	)).Error
}

func (a *Adapter) ruleToPolicyArray(line CasbinRule) []string {
	p := []string{line.Ptype, line.V0, line.V1, line.V2, line.V3, line.V4, line.V5}
	idx := len(p) - 1
	for idx >= 0 && p[idx] == "" {
		idx--
	}
	return p[:idx+1]
}

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

func (a *Adapter) addRule(line CasbinRule) error {
	return a.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error
}

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

func (a *Adapter) SavePolicy(model model.Model) error {
	tx := a.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}

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

func (a *Adapter) AddPolicy(sec string, ptype string, rule []string) error {
	return a.addRule(a.savePolicyLine(ptype, rule))
}

func (a *Adapter) RemovePolicy(sec string, ptype string, rule []string) error {
	return a.deleteRuleRaw(a.savePolicyLine(ptype, rule))
}

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
