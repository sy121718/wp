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

// HasPermissionPoliciesTx 在调用方事务内判断某个权限码是否还被角色引用。
//
// 内存 enforcer 是策略的一份副本，看不到本事务里刚写入的绑定，也看不到
// 别的连接尚未提交的改动。删除权限点前的引用校验要走这里，读的是事务内的
// 政策表本身，判完到删之间不会出现「刚被绑上却当成没人用」。
func HasPermissionPoliciesTx(ctx context.Context, tx *gorm.DB, code string) (bool, error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if code == "" {
		return false, errors.New("权限编码不能为空")
	}
	var count int64
	if err := tx.WithContext(ctx).Model(&CasbinRule{}).
		Where("ptype = ? AND v3 = ?", policyTypePermission, code).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("查询权限策略失败: %w", err)
	}
	return count > 0, nil
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

// ReplaceRolePermissionsTx 在调用方事务内把某个角色的 p 策略整批换成 policies。
//
// 与 ReplaceRolePermissions 的区别只在「用哪种连接」：这里写的是调用方的事务，
// 所以「授予某个权限码」能和服务端的其它写入（含被授予实体的行锁）落在同一个事务里，
// 不会出现「锁住了实体、策略却写在另一条连接上」的裂缝。
//
// 不动内存 enforcer：提交后由调用方 ReloadPolicy 让改动生效（与既有写端一致）。
func ReplaceRolePermissionsTx(ctx context.Context, tx *gorm.DB, roleCode string, policies [][3]string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if roleCode == "" {
		return false, errors.New("角色编码不能为空")
	}
	return replaceSubjectPoliciesTx(ctx, tx, roleCode, policies)
}

// ReplaceUserPermissionsTx 在调用方事务内把某个用户的直接 p 策略整批换成 policies。
func ReplaceUserPermissionsTx(ctx context.Context, tx *gorm.DB, userID string, policies [][3]string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if userID == "" {
		return false, errors.New("用户 ID 不能为空")
	}
	return replaceSubjectPoliciesTx(ctx, tx, userID, policies)
}

// ReplaceRoleUsersTx 在调用方事务内把某个角色的成员绑定整批换成 userIDs。
//
// 成员绑定是 grouping 策略（ptype=g），不是 p 策略：
// 它决定「谁属于这个角色」，与「角色能做什么」是两张不同的表行，不能混用上面的替换函数。
//
// 不动内存 enforcer：提交后由调用方 ReloadPolicy 让改动生效（与既有写端一致）。
func ReplaceRoleUsersTx(ctx context.Context, tx *gorm.DB, roleCode string, userIDs []string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if roleCode == "" {
		return false, errors.New("角色编码不能为空")
	}

	want := make(map[string]struct{}, len(userIDs))
	for _, uid := range userIDs {
		if uid == "" {
			continue
		}
		want[uid] = struct{}{}
	}

	var existing []CasbinRule
	if err = tx.WithContext(ctx).
		Where("ptype = ? AND v1 = ?", policyTypeGrouping, roleCode).
		Order("id ASC").Find(&existing).Error; err != nil {
		return false, fmt.Errorf("查询角色现有成员绑定失败: %w", err)
	}

	have := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		have[row.V0] = struct{}{}
	}
	if len(want) == len(have) {
		same := true
		for uid := range want {
			if _, ok := have[uid]; !ok {
				same = false
				break
			}
		}
		if same {
			return false, nil
		}
	}

	for _, row := range existing {
		if _, keep := want[row.V0]; keep {
			continue
		}
		res := tx.WithContext(ctx).
			Where("ptype = ? AND v0 = ? AND v1 = ?", policyTypeGrouping, row.V0, roleCode).
			Delete(&CasbinRule{})
		if res.Error != nil {
			return false, fmt.Errorf("删除角色旧成员绑定失败: %w", res.Error)
		}
	}
	for uid := range want {
		if _, keep := have[uid]; keep {
			continue
		}
		line := CasbinRule{Ptype: policyTypeGrouping, V0: uid, V1: roleCode}
		if err = tx.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error; err != nil {
			return false, fmt.Errorf("添加角色成员绑定失败: %w", err)
		}
	}
	return true, nil
}

// ReplaceUserRoleBindingsTx 在调用方事务内把某个用户的角色绑定整批换成 roleCodes。
//
// 与 ReplaceRoleUsersTx 是同一张表（ptype=g）的两侧：那边按角色收成员，这边按用户收角色。
// 两个入口都写 sys_casbin_rule，都必须写在调用方事务里，否则「锁住了人/角色，绑定却写
// 在 enforcer 自己的连接上」：本事务回滚时绑定不会跟着回滚。
func ReplaceUserRoleBindingsTx(ctx context.Context, tx *gorm.DB, userID string, roleCodes []string) (changed bool, err error) {
	if tx == nil {
		return false, errors.New("事务句柄不能为空")
	}
	if userID == "" {
		return false, errors.New("用户 ID 不能为空")
	}

	want := make(map[string]struct{}, len(roleCodes))
	for _, code := range roleCodes {
		if code == "" {
			continue
		}
		want[code] = struct{}{}
	}

	var existing []CasbinRule
	if err = tx.WithContext(ctx).
		Where("ptype = ? AND v0 = ?", policyTypeGrouping, userID).
		Order("id ASC").Find(&existing).Error; err != nil {
		return false, fmt.Errorf("查询用户现有角色绑定失败: %w", err)
	}

	have := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		have[row.V1] = struct{}{}
	}
	if len(want) == len(have) {
		same := true
		for code := range want {
			if _, ok := have[code]; !ok {
				same = false
				break
			}
		}
		if same {
			return false, nil
		}
	}

	for _, row := range existing {
		if _, keep := want[row.V1]; keep {
			continue
		}
		res := tx.WithContext(ctx).
			Where("ptype = ? AND v0 = ? AND v1 = ?", policyTypeGrouping, userID, row.V1).
			Delete(&CasbinRule{})
		if res.Error != nil {
			return false, fmt.Errorf("删除用户旧角色绑定失败: %w", res.Error)
		}
	}
	for code := range want {
		if _, keep := have[code]; keep {
			continue
		}
		line := CasbinRule{Ptype: policyTypeGrouping, V0: userID, V1: code}
		if err = tx.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error; err != nil {
			return false, fmt.Errorf("添加用户角色绑定失败: %w", err)
		}
	}
	return true, nil
}

// replaceSubjectPoliciesTx 是上面两个 Tx 版本的共同实现：删掉该 subject 现有 p 策略，
// 再写入 want 里的每一条。返回是否真的改动了政策表，调用方据此决定要不要 ReloadPolicy。
func replaceSubjectPoliciesTx(ctx context.Context, tx *gorm.DB, subject string, policies [][3]string) (changed bool, err error) {
	var existing []CasbinRule
	if err = tx.WithContext(ctx).
		Where("ptype = ? AND v0 = ?", policyTypePermission, subject).
		Order("id ASC").Find(&existing).Error; err != nil {
		return false, fmt.Errorf("查询现有策略失败: %w", err)
	}

	want := make(map[string]struct{}, len(policies))
	for _, p := range policies {
		want[p[0]+"\x00"+p[1]+"\x00"+p[2]] = struct{}{}
	}
	have := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		have[row.V1+"\x00"+row.V2+"\x00"+row.V3] = struct{}{}
	}
	if len(want) == len(have) {
		same := true
		for key := range want {
			if _, ok := have[key]; !ok {
				same = false
				break
			}
		}
		if same {
			// 一次都没变：不写、不删，调用方也就没必要重载。
			return false, nil
		}
	}

	if err = tx.WithContext(ctx).
		Where("ptype = ? AND v0 = ?", policyTypePermission, subject).
		Delete(&CasbinRule{}).Error; err != nil {
		return false, fmt.Errorf("删除旧策略失败: %w", err)
	}
	for _, p := range policies {
		line := CasbinRule{Ptype: policyTypePermission, V0: subject, V1: p[0], V2: p[1], V3: p[2]}
		if err = tx.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).Create(&line).Error; err != nil {
			return false, fmt.Errorf("写入策略失败: %w", err)
		}
	}
	return true, nil
}
