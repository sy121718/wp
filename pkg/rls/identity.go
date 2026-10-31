package rls

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

type Identity struct {
	SessionUser string `gorm:"column:session_user"`

	CurrentUser string `gorm:"column:current_user"`

	IsSuperuser bool `gorm:"column:is_superuser"`

	BypassRLS bool `gorm:"column:bypass_rls"`

	RLSTables int `gorm:"column:rls_tables"`

	OwnedRLSTables int `gorm:"column:owned_rls_tables"`
}

func (i Identity) BypassesRLS() bool {
	return i.IsSuperuser || i.BypassRLS
}

func (i Identity) Verdict() string {
	if i.BypassesRLS() {
		reason := "超级用户"
		if i.BypassRLS && !i.IsSuperuser {
			reason = "带 BYPASSRLS"
		}
		return fmt.Sprintf("连接角色 %s（session_user=%s）是%s，会无条件绕过 RLS：%d 个对象上的工程隔离策略当前一行都挡不住",
			i.CurrentUser, i.SessionUser, reason, i.RLSTables)
	}
	extra := ""
	if i.OwnedRLSTables > 0 {
		extra = fmt.Sprintf("，其中 %d 张表属主是它自己（靠 FORCE 约束，不是靠超级用户豁免）", i.OwnedRLSTables)
	}
	return fmt.Sprintf("连接角色 %s 不绕过 RLS：%d 个对象上的工程隔离策略生效%s", i.CurrentUser, i.RLSTables, extra)
}

const identityQuery = `SELECT
    session_user::text AS session_user,
    current_user::text AS current_user,
    r.rolsuper AS is_superuser,
    r.rolbypassrls AS bypass_rls,
    (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
       WHERE n.nspname = current_schema() AND c.relkind IN ('r','p') AND c.relrowsecurity) AS rls_tables,
    (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
       WHERE n.nspname = current_schema() AND c.relkind IN ('r','p') AND c.relrowsecurity
         AND c.relowner = (SELECT oid FROM pg_roles WHERE rolname = current_user)) AS owned_rls_tables
FROM pg_roles r
WHERE r.rolname = current_user`

func ProbeIdentity(ctx context.Context, db *gorm.DB) (Identity, error) {
	var identity Identity
	if db == nil {
		return identity, fmt.Errorf("rls: 数据库句柄为空，无法探测连接身份")
	}
	if err := db.WithContext(ctx).Raw(identityQuery).Scan(&identity).Error; err != nil {
		return identity, err
	}
	if identity.CurrentUser == "" {

		return identity, fmt.Errorf("rls: 未取到当前连接角色（session_user/current_user 为空）")
	}
	return identity, nil
}
