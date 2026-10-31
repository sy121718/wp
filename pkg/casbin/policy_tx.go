package casbin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	policyTypePermission = "p"
	policyTypeGrouping   = "g"
	policyTypeRoleActive = "g2"
)

const roleActiveMarker = "active"

func ReplacePermissionDefinitionTx(ctx context.Context, tx *gorm.DB, code, path, method string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if code == "" || path == "" || method == "" {
		return false, errors.New("权限编码、请求路径和方法不能为空")
	}
	method = strings.ToUpper(method)

	var existing []CasbinRule
	if err = tx.WithContext(ctx).
		Where("ptype = ? AND v3 = ?", policyTypePermission, code).
		Order("id ASC").Find(&existing).Error; err != nil {
		return false, fmt.Errorf("查询权限策略失败: %w", err)
	}

	subjects := make([]string, 0, len(existing))
	seen := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		if row.V0 == "" {
			continue
		}
		if _, ok := seen[row.V0]; ok {
			continue
		}
		seen[row.V0] = struct{}{}
		subjects = append(subjects, row.V0)
	}
	if len(subjects) == 0 {
		return false, nil
	}

	if err = tx.WithContext(ctx).
		Where("ptype = ? AND v3 = ?", policyTypePermission, code).
		Delete(&CasbinRule{}).Error; err != nil {
		return false, fmt.Errorf("删除权限旧策略失败: %w", err)
	}
	for _, sub := range subjects {
		line := CasbinRule{Ptype: policyTypePermission, V0: sub, V1: path, V2: method, V3: code}
		if err = tx.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error; err != nil {
			return false, fmt.Errorf("写入权限新策略失败（%s %s）：%w", method, path, err)
		}
	}
	return true, nil
}

func ActivateRoleTx(ctx context.Context, tx *gorm.DB, roleCode string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if roleCode == "" {
		return false, errors.New("角色编码不能为空")
	}

	line := CasbinRule{Ptype: policyTypeRoleActive, V0: roleCode, V1: roleActiveMarker}
	result := tx.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).Create(&line)
	if result.Error != nil {
		return false, fmt.Errorf("启用角色失败: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func DeactivateRoleTx(ctx context.Context, tx *gorm.DB, roleCode string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if roleCode == "" {
		return false, errors.New("角色编码不能为空")
	}

	result := tx.WithContext(ctx).
		Where("ptype = ? AND v0 = ? AND v1 = ?", policyTypeRoleActive, roleCode, roleActiveMarker).
		Delete(&CasbinRule{})
	if result.Error != nil {
		return false, fmt.Errorf("禁用角色失败: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func DeleteRoleAllPoliciesTx(ctx context.Context, tx *gorm.DB, roleCode string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if roleCode == "" {
		return false, errors.New("角色编码不能为空")
	}

	scopes := []struct {
		cond string
		args []any
	}{
		{"ptype = ? AND v0 = ?", []any{policyTypePermission, roleCode}},
		{"ptype = ? AND v1 = ?", []any{policyTypeGrouping, roleCode}},
		{"ptype = ? AND v0 = ? AND v1 = ?", []any{policyTypeRoleActive, roleCode, roleActiveMarker}},
	}

	var total int64
	for _, scope := range scopes {
		res := tx.WithContext(ctx).Where(scope.cond, scope.args...).Delete(&CasbinRule{})
		if res.Error != nil {
			return false, fmt.Errorf("删除角色策略失败: %w", res.Error)
		}
		total += res.RowsAffected
	}
	return total > 0, nil
}

func DeleteUserAllPoliciesTx(ctx context.Context, tx *gorm.DB, userID string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if userID == "" {
		return false, errors.New("用户 ID 不能为空")
	}

	scopes := []struct {
		cond string
		args []any
	}{
		{"ptype = ? AND v0 = ?", []any{policyTypePermission, userID}},
		{"ptype = ? AND v0 = ?", []any{policyTypeGrouping, userID}},
	}

	var total int64
	for _, scope := range scopes {
		res := tx.WithContext(ctx).Where(scope.cond, scope.args...).Delete(&CasbinRule{})
		if res.Error != nil {
			return false, fmt.Errorf("删除用户策略失败: %w", res.Error)
		}
		total += res.RowsAffected
	}
	return total > 0, nil
}

func ReplacePermissionDefinition(code, path, method string) error {
	return runPolicyTx(func(ctx context.Context, tx *gorm.DB) (bool, error) {
		return ReplacePermissionDefinitionTx(ctx, tx, code, path, method)
	})
}

func ActivateRole(roleCode string) error {
	return runPolicyTx(func(ctx context.Context, tx *gorm.DB) (bool, error) {
		return ActivateRoleTx(ctx, tx, roleCode)
	})
}

func DeactivateRole(roleCode string) error {
	return runPolicyTx(func(ctx context.Context, tx *gorm.DB) (bool, error) {
		return DeactivateRoleTx(ctx, tx, roleCode)
	})
}

func runPolicyTx(fn func(ctx context.Context, tx *gorm.DB) (bool, error)) error {
	db, err := policyDB()
	if err != nil {
		return err
	}

	changed := false
	if err := db.Transaction(func(tx *gorm.DB) error {
		c, err := fn(context.Background(), tx)
		if err != nil {
			return err
		}
		changed = c
		return nil
	}); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return ReloadPolicy()
}

func policyDB() (*gorm.DB, error) {
	e := GetEnforcer()
	if e == nil {
		return nil, fmt.Errorf("casbin 未初始化")
	}
	a, ok := e.GetAdapter().(*Adapter)
	if !ok || a == nil {
		return nil, fmt.Errorf("casbin 适配器类型不支持事务：%T", e.GetAdapter())
	}
	if a.db == nil {
		return nil, fmt.Errorf("casbin 适配器未绑定数据库句柄")
	}
	return a.db, nil
}
