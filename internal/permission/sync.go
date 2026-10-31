package permission

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

const batchSize = 80

const tableSysPermission = "sys_permission"

type SyncResult struct {
	Declared int

	Exempt int

	Inserted int64

	PathFixed int64

	PolicyFixed int64

	SuperPolicies int64

	Unmanaged []string
}

func (r SyncResult) Changed() bool {
	return r.Inserted > 0 || r.PathFixed > 0 || r.PolicyFixed > 0 || r.SuperPolicies > 0
}

func (r SyncResult) String() string {
	return fmt.Sprintf("声明 %d 条（豁免 %d 条）；新增权限点 %d、修正权限点路径 %d、对齐策略 %d、补超管策略 %d",
		r.Declared, r.Exempt, r.Inserted, r.PathFixed, r.PolicyFixed, r.SuperPolicies)
}

func SyncToDB(ctx context.Context, db *gorm.DB) (res SyncResult, err error) {
	if db == nil {
		return res, fmt.Errorf("permission.SyncToDB 收到 nil 数据库句柄")
	}
	rows := Snapshot()
	res.Declared = len(rows)
	res.Exempt = len(Exempts())
	if len(rows) == 0 {

		return res, nil
	}

	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(rows); start += batchSize {
			end := start + batchSize
			if end > len(rows) {
				end = len(rows)
			}
			batch := rows[start:end]

			n, ierr := insertMissingPermissions(tx, batch)
			if ierr != nil {
				return ierr
			}
			res.Inserted += n

			n, ierr = fixPermissionRoutes(tx, batch)
			if ierr != nil {
				return ierr
			}
			res.PathFixed += n
		}

		if res.PathFixed > 0 {
			n, ierr := alignPolicyRoutes(tx, rows)
			if ierr != nil {
				return ierr
			}
			res.PolicyFixed = n
		}

		n, ierr := insertSuperadminPolicies(tx)
		if ierr != nil {
			return ierr
		}
		res.SuperPolicies = n

		if un, uerr := listUnmanagedPermissions(tx, rows); uerr == nil {
			res.Unmanaged = un
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	return res, nil
}

func insertMissingPermissions(tx *gorm.DB, rows []RouteSpec) (int64, error) {
	var sb strings.Builder
	sb.WriteString("INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time) VALUES ")
	args := make([]any, 0, len(rows)*5)
	for i, r := range rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?, ?, ?, 1, 0, NOW(), 0, NOW())")
		args = append(args, string(r.Perm), r.Name, r.Module, r.Path, r.Method)
	}
	sb.WriteString(" ON CONFLICT (permission_code) DO NOTHING")
	return tx.Exec(sb.String(), args...).RowsAffected, nil
}

func fixPermissionRoutes(tx *gorm.DB, rows []RouteSpec) (int64, error) {
	var sb strings.Builder
	sb.WriteString("UPDATE sys_permission p SET api_path = v.api_path, api_method = v.api_method, update_time = NOW() FROM (VALUES ")
	args := make([]any, 0, len(rows)*3)
	for i, r := range rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("(?, ?, ?)")
		args = append(args, string(r.Perm), r.Path, r.Method)
	}
	sb.WriteString(") AS v(permission_code, api_path, api_method) WHERE p.permission_code = v.permission_code AND (p.api_path <> v.api_path OR p.api_method <> v.api_method)")
	return tx.Exec(sb.String(), args...).RowsAffected, nil
}

func alignPolicyRoutes(tx *gorm.DB, rows []RouteSpec) (int64, error) {
	codes := make([]any, 0, len(rows))
	ph := make([]string, 0, len(rows))
	for _, r := range rows {
		ph = append(ph, "?")
		codes = append(codes, string(r.Perm))
	}
	sql := "UPDATE sys_casbin_rule r SET v1 = p.api_path, v2 = p.api_method FROM sys_permission p " +
		"WHERE r.ptype = 'p' AND r.v3 = p.permission_code AND r.v3 IN (" + strings.Join(ph, ",") + ") " +
		"AND (r.v1 <> p.api_path OR r.v2 <> p.api_method)"
	return tx.Exec(sql, codes...).RowsAffected, nil
}

func listUnmanagedPermissions(tx *gorm.DB, rows []RouteSpec) ([]string, error) {
	managed := make(map[string]bool, len(rows))
	for _, r := range rows {
		managed[string(r.Perm)] = true
	}

	var found []struct {
		Code   string `gorm:"column:code"`
		Method string `gorm:"column:method"`
		Path   string `gorm:"column:path"`
	}
	if err := tx.Table(tableSysPermission).
		Select("permission_code AS code, api_method AS method, api_path AS path").
		Where("status = 1 AND api_path <> ''").
		Scan(&found).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0)
	for _, f := range found {
		if !managed[f.Code] {
			out = append(out, f.Code+" "+f.Method+" "+f.Path)
		}
	}
	sort.Strings(out)
	return out, nil
}

func insertSuperadminPolicies(tx *gorm.DB) (int64, error) {
	const sql = "INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3) " +
		"SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code " +
		"FROM sys_admin a CROSS JOIN sys_permission p " +
		"WHERE a.is_admin = 1 AND p.status = 1 AND p.api_path <> '' " +
		"AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) " +
		"AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code)"
	return tx.Exec(sql).RowsAffected, nil
}
