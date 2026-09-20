package database

import (
	"context"
	"fmt"
	"strings"

	"go_wp/pkg/logger"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

// 本文件是启动期的 RLS 连接身份探针（审计 DB-04）。
//
// 它回答一个配置与文档都答不了的问题：**这条连接到底会不会绕过 RLS**。
// 迁移 199 / 215 把策略铺到了 53 个对象上，但 PostgreSQL 的超级用户无条件绕过 RLS，
// FORCE 只约束表属主 —— 于是「策略铺好了」与「隔离生效」之间差着一个角色属性。
// 探针每次启动都读 pg_roles.rolsuper / rolbypassrls 并打日志，结论只有两种：
//
//   - 不绕过：策略生效（内容见 rls.Identity.Verdict）；
//   - 绕过：INFO 打结论 + WARN 说明当前隔离不生效；
//     若 database.require_rls_role=true，直接返回 error 让启动失败。
//
// 默认 require_rls_role=false 是刻意的：迁移与运维脚本用管理连接（超级用户）执行 DDL，
// 而它们走的是同一个 database 组件；一旦默认 fail fast，正常运维会被自己的门禁拦住。
// 切换成应用角色之后，把该项置 true 才算真正闭环。

// isPostgresDialector 判断 gorm 方言名是不是 PostgreSQL。
//
// 探针要读 pg_roles / pg_class，MySQL 上没有这些视图 —— 非 PG 必须整体跳过而不是
// 报错（MySQL 是历史兼容驱动，不该因为一个 PG 专属的检查而无法启动）。
func isPostgresDialector(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "postgres", "postgresql", "pgx":
		return true
	default:
		return false
	}
}

// rlsRoleBypassError 在 require_rls_role=true 且连接角色会绕过 RLS 时的启动失败错误。
//
// 错误里带上实际角色名与 session_user：DBA 要能一眼看出「配置里写的是谁、连上去的又是谁」，
// 否则最常见的那种情况（改了 database.user 但环境变量 GOWP_DATABASE_USER 还指着旧值）
// 只能靠猜。
func rlsRoleBypassError(identity rls.Identity) error {
	reason := "超级用户"
	if identity.BypassRLS && !identity.IsSuperuser {
		reason = "带 BYPASSRLS"
	}
	return fmt.Errorf(
		"database.require_rls_role=true，但连接角色 %s（session_user=%s）是%s：它会无条件绕过 RLS，工程隔离不会生效；"+
			"请把 database.user 换成非超级应用角色（bash scripts/rls-role-setup.sh），或把 require_rls_role 改回 false",
		identity.CurrentUser, identity.SessionUser, reason)
}

// CheckRLSIdentity 执行启动期 RLS 连接身份探针。
//
// 行为（每一步都有明确理由，改动前先读上面的说明）：
//  1. 非 PostgreSQL 方言 → INFO 说明跳过，返回 nil；
//  2. 探针查询本身失败 → require=true 时返回 error（无法证明隔离生效，不能假装通过），
//     否则 WARN 后继续；
//  3. 无论结论如何，先以 INFO 打印完整结论（角色名 + rolsuper + rolbypassrls + 策略覆盖数）；
//  4. 连接角色会绕过 RLS → require=true 返回 error（启动失败），否则 WARN。
//
// 返回 error 时不修改任何全局状态：由 Init 负责关连接并把错误往上抛。
func CheckRLSIdentity(ctx context.Context, db *gorm.DB, requireRLSRole bool) error {
	if db == nil {
		return nil
	}

	driverName := ""
	if db.Dialector != nil {
		driverName = db.Dialector.Name()
	}
	if !isPostgresDialector(driverName) {
		logger.Scene("init").With("driver", driverName).
			Info("当前驱动不是 PostgreSQL，跳过 RLS 连接身份探针")
		return nil
	}

	identity, err := rls.ProbeIdentity(ctx, db)
	if err != nil {
		if requireRLSRole {
			return fmt.Errorf("RLS 连接身份探针失败（database.require_rls_role=true，无法确认隔离生效）: %w", err)
		}
		logger.Scene("init").With("err", err).
			Warn("RLS 连接身份探针失败，无法确认工程隔离是否生效")
		return nil
	}

	entry := logger.Scene("init").
		With("sessionUser", identity.SessionUser).
		With("currentUser", identity.CurrentUser).
		With("rolsuper", identity.IsSuperuser).
		With("rolbypassrls", identity.BypassRLS).
		With("rlsTables", identity.RLSTables).
		With("ownedRlsTables", identity.OwnedRLSTables)

	if !identity.BypassesRLS() {
		entry.Info(identity.Verdict())
		return nil
	}

	entry.Info(identity.Verdict())
	if requireRLSRole {
		return rlsRoleBypassError(identity)
	}
	logger.Scene("init").
		With("currentUser", identity.CurrentUser).
		With("rolsuper", identity.IsSuperuser).
		With("rolbypassrls", identity.BypassRLS).
		Warn("连接角色会绕过 RLS，工程隔离当前不生效：切到非超级应用角色后把 database.require_rls_role 置 true，启动期会替你把这道门禁守住")
	return nil
}
