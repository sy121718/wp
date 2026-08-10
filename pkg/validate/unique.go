package validate

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var columnNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// IsUnique 检查字段值是否唯一（不存在）。
// fieldMap 为字段白名单映射：业务字段 -> 数据库列名。
func IsUnique(db *gorm.DB, model any, field string, value any, fieldMap map[string]string) (bool, error) {
	column, err := resolveColumn(field, fieldMap)
	if err != nil {
		return false, err
	}

	var count int64
	if err := db.Model(model).Where(clause.Eq{
		Column: clause.Column{Name: column},
		Value:  value,
	}).Count(&count).Error; err != nil {
		return false, fmt.Errorf("检查唯一性失败: %w", err)
	}
	return count == 0, nil
}

// IsExists 检查字段值是否存在。
// fieldMap 为字段白名单映射：业务字段 -> 数据库列名。
func IsExists(db *gorm.DB, model any, field string, value any, fieldMap map[string]string) (bool, error) {
	column, err := resolveColumn(field, fieldMap)
	if err != nil {
		return false, err
	}

	var count int64
	if err := db.Model(model).Where(clause.Eq{
		Column: clause.Column{Name: column},
		Value:  value,
	}).Count(&count).Error; err != nil {
		return false, fmt.Errorf("检查存在性失败: %w", err)
	}
	return count > 0, nil
}

// IsUniqueExclude 排除当前记录后检查唯一性（用于更新场景）。
// fieldMap 为字段白名单映射：业务字段 -> 数据库列名。
func IsUniqueExclude(db *gorm.DB, model any, field string, value any, excludeID uint, fieldMap map[string]string) (bool, error) {
	column, err := resolveColumn(field, fieldMap)
	if err != nil {
		return false, err
	}

	var count int64
	if err := db.Model(model).
		Where(clause.Eq{Column: clause.Column{Name: column}, Value: value}).
		Where(clause.Neq{Column: clause.Column{Name: "id"}, Value: excludeID}).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("排除主键后检查唯一性失败: %w", err)
	}
	return count == 0, nil
}

// IsUniqueExcludeField 排除指定字段后检查唯一性。
// fieldMap 为字段白名单映射：业务字段 -> 数据库列名。
func IsUniqueExcludeField(db *gorm.DB, model any, field string, value any, excludeField string, excludeValue any, fieldMap map[string]string) (bool, error) {
	column, err := resolveColumn(field, fieldMap)
	if err != nil {
		return false, err
	}
	excludeColumn, err := resolveColumn(excludeField, fieldMap)
	if err != nil {
		return false, err
	}

	var count int64
	if err := db.Model(model).
		Where(clause.Eq{Column: clause.Column{Name: column}, Value: value}).
		Where(clause.Neq{Column: clause.Column{Name: excludeColumn}, Value: excludeValue}).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("排除字段后检查唯一性失败: %w", err)
	}
	return count == 0, nil
}

func resolveColumn(field string, fieldMap map[string]string) (string, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return "", fmt.Errorf("字段名不能为空")
	}
	if len(fieldMap) == 0 {
		return "", fmt.Errorf("字段白名单映射不能为空")
	}

	column, ok := fieldMap[field]
	if !ok {
		return "", fmt.Errorf("字段不在白名单中: %s", field)
	}
	column = strings.TrimSpace(column)
	if !columnNamePattern.MatchString(column) {
		return "", fmt.Errorf("非法字段映射: %s", column)
	}

	return column, nil
}
