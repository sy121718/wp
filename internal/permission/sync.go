// sync.go — 启动期把声明幂等 upsert 进数据库（审计 SEC-011 的长期方案第 2、3 条）。
//
// 碰哪些表、碰哪些列（这是「不覆盖人工授权」的关键，改动前先读这段）：
//
//	sys_permission
//	  · 只 INSERT **缺失的** permission_code（ON CONFLICT (permission_code) DO NOTHING）；
//	    已存在的行只修正 api_path / api_method / update_time 三列 —— 它们是「这个权限点
//	    绑在哪条路由上」的事实列，跟随代码声明走；
//	  · 绝不改写 permission_name / module / status / remark：人工改过的中文名、人工禁用的
//	    权限点（status=0）都保留。因此「权限点存在」与「谁被授了权」是两件事，这里只碰前者。
//
//	sys_casbin_rule
//	  · 只 INSERT 超管（sys_admin.is_admin=1）缺失的 p 行，与迁移 051 同一条 SQL 语义；
//	  · 唯一会 UPDATE 的场合是权限点路径被改写时，把引用该 code 的策略行的 v1/v2 对齐到
//	    权限点当前路径（否则改名后角色策略会指向不存在的路径，角色用户静默 403）。
//	    **v0（授权主体）与 v3（权限点代码）永不变** —— 没有「把谁的权撤掉」，也没有
//	    「给谁多授一条」；
//	  · 绝不 DELETE 任何行：人工在后台撤销的授权不会被自动种回来，代码里删掉的权限点也
//	    不会连带清库（清理属运维动作，不在启动链上做）。
//
// 幂等：三条语句都有「不存在才写 / 值不同才改」的守卫，重复启动写入行数为 0。
package permission

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// batchSize 单条 SQL 携带的声明行数（避免 274 行声明拼出一条几十 KB 的语句）。
const batchSize = 80

// SyncResult 一次同步的结果（写启动日志用：能说清「这次启动补了什么」）。
type SyncResult struct {
	// Declared 本次装配声明的授权路由数。
	Declared int
	// Exempt 显式豁免的路由数（声明中 perm 为 Exempt 的）。
	Exempt int
	// Inserted 新补的权限点行数。
	Inserted int64
	// PathFixed 因声明路径/方法与库中不一致而被修正的权限点行数。
	PathFixed int64
	// PolicyFixed 因权限点路径改写而跟随对齐的策略行数。
	PolicyFixed int64
	// SuperPolicies 新补的超管策略行数。
	SuperPolicies int64
	// Unmanaged 库中启用、有 api_path、但**本次装配没有声明**的权限点（"code METHOD path"）。
	//
	// 双轨期（历史 seed + 声明式注册并存）的可见性保障：它们保留不动（可能对应页面路由入口
	// 或历史 seed），但启动日志要能把这份清单摆出来 —— 否则「代码里删了权限点、库里还在」
	// 这类漂移永远没人看见。
	Unmanaged []string
}

// Changed 是否对数据库产生了写入（用于「没变化就不打日志」）。
func (r SyncResult) Changed() bool {
	return r.Inserted > 0 || r.PathFixed > 0 || r.PolicyFixed > 0 || r.SuperPolicies > 0
}

// String 渲染成一行启动日志文案。
func (r SyncResult) String() string {
	return fmt.Sprintf("声明 %d 条（豁免 %d 条）；新增权限点 %d、修正权限点路径 %d、对齐策略 %d、补超管策略 %d",
		r.Declared, r.Exempt, r.Inserted, r.PathFixed, r.PolicyFixed, r.SuperPolicies)
}

// SyncToDB 把当前装配期声明的权限点同步进库（幂等）。
//
// 必须在**路由装配完成之后**调用：声明表是注册动作的产物。
func SyncToDB(ctx context.Context, db *gorm.DB) (res SyncResult, err error) {
	if db == nil {
		return res, fmt.Errorf("permission.SyncToDB 收到 nil 数据库句柄")
	}
	rows := Snapshot()
	res.Declared = len(rows)
	res.Exempt = len(Exempts())
	if len(rows) == 0 {
		// 没有声明 = 没有挂在授权面上的路由（例如只起了静态面），不写库。
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

		// 观测项：失败不影响同步结论（同步本身已经完成）。
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

// insertMissingPermissions 补缺失的权限点（已存在的不动）。
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

// fixPermissionRoutes 把已存在权限点的「路由事实列」对齐到声明。
//
// 只更新这三列：api_path / api_method / update_time。名字、模块、状态、备注一概不动。
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

// alignPolicyRoutes 权限点路径被改写后，让引用它的策略行跟上（v0 / v3 不变，只对齐 v1/v2）。
//
// 限定在本次声明的权限点集合内 —— 不越界去「顺手修正」代码没有声明的历史行。
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

// listUnmanagedPermissions 列出库中「有 api_path 但本次没有声明」的启用权限点。
//
// 纯观测：不阻断、不清理。声明式注册接管的是「路由 → 权限点」这一侧，库里多出来的条目
// 要么是历史 seed 的残留（如被删掉的库存缓存接口），要么是页面路由 / 中间件按指定路径
// enforce 时用的权限点（如 seo:audit），删掉反而会打断授权。
func listUnmanagedPermissions(tx *gorm.DB, rows []RouteSpec) ([]string, error) {
	managed := make(map[string]bool, len(rows))
	for _, r := range rows {
		managed[string(r.Perm)] = true
	}
	var found []struct {
		Code   string
		Method string
		Path   string
	}
	if err := tx.Raw("SELECT permission_code AS code, api_method AS method, api_path AS path " +
		"FROM sys_permission WHERE status = 1 AND api_path <> ''").Scan(&found).Error; err != nil {
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

// insertSuperadminPolicies 把「超管 = 全部启用权限点」补齐（与迁移 051 同一条 SQL 语义）。
//
// 只针对 is_admin=1 的账号，只补缺失的四元组；非超管的角色 / 用户授权不受影响。
func insertSuperadminPolicies(tx *gorm.DB) (int64, error) {
	const sql = "INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3) " +
		"SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code " +
		"FROM sys_admin a CROSS JOIN sys_permission p " +
		"WHERE a.is_admin = 1 AND p.status = 1 AND p.api_path <> '' " +
		"AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) " +
		"AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code)"
	return tx.Exec(sql).RowsAffected, nil
}
