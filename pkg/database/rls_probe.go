package database

import (
	"context"
	"fmt"
	"strings"

	"go_wp/pkg/logger"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

func isPostgresDialector(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "postgres", "postgresql", "pgx":
		return true
	default:
		return false
	}
}

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
