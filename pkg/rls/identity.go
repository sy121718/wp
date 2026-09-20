package rls

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Identity 是当前数据库连接的「身份 + 会不会绕过 RLS」快照（审计 DB-04）。
//
// 为什么需要它：**策略铺好了不等于隔离生效**。迁移 199 / 215 在 53 个对象上装了
// ROW LEVEL SECURITY + FORCE，但 PostgreSQL 的超级用户**无条件绕过** RLS ——
// FORCE 约束的是表属主，约束不了 superuser / BYPASSRLS 角色。所以「这一轮跑在哪个身份上」
// 决定了那些断言与验收是不是真的，而它只能从库里读出来（配置里写的是用户名，
// 角色属性随时可以被 DBA 改掉）。
//
// 生产启动、测试夹具、运维脚本都从这一个探针取值，避免三处各写一条 SQL 而慢慢漂移。
type Identity struct {
	// SessionUser 是建连时用于认证的角色（SET ROLE 之后不变）。
	SessionUser string `gorm:"column:session_user"`
	// CurrentUser 是当前生效角色（SET ROLE 之后会变）——RLS 的绕过判定看的是它。
	CurrentUser string `gorm:"column:current_user"`
	// IsSuperuser 对应 pg_roles.rolsuper。
	IsSuperuser bool `gorm:"column:is_superuser"`
	// BypassRLS 对应 pg_roles.rolbypassrls。
	BypassRLS bool `gorm:"column:bypass_rls"`
	// RLSTables 是当前 schema 里已启用 RLS 的表与分区数（策略铺开的广度）。
	RLSTables int `gorm:"column:rls_tables"`
	// OwnedRLSTables 是其中属于 current_user 的数量。
	// 属主本身会被 FORCE 约束，但 superuser / BYPASSRLS 仍会绕过去 —— 两者分开看。
	OwnedRLSTables int `gorm:"column:owned_rls_tables"`
}

// BypassesRLS 报告这个身份是否会无条件绕过 RLS。
//
// 返回 true 时，不管策略铺了多少张表、model 里包了多少次 InProjectScope，
// 工程隔离都不生效 —— 这正是 DB-04 要消除的「文档说生效、现场没生效」。
func (i Identity) BypassesRLS() bool {
	return i.IsSuperuser || i.BypassRLS
}

// Verdict 给出人类可读的结论（启动日志用它，运维不必自己拼字段）。
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

// identityQuery 只读，不改任何会话状态；子查询一律限定 current_schema() ——
// pg_class 是全库的，并发测试或残留 schema 会把同名表一起算进来。
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

// ProbeIdentity 读取当前连接的身份与 RLS 绕过属性。
//
// PG 专用：pg_roles / pg_class 是 PostgreSQL 的系统视图。非 PG 驱动由调用方
// （pkg/database 的启动探针）自行跳过，本函数不做方言判断。
func ProbeIdentity(ctx context.Context, db *gorm.DB) (Identity, error) {
	var identity Identity
	if db == nil {
		return identity, fmt.Errorf("rls: 数据库句柄为空，无法探测连接身份")
	}
	if err := db.WithContext(ctx).Raw(identityQuery).Scan(&identity).Error; err != nil {
		return identity, err
	}
	if identity.CurrentUser == "" {
		// 查不到行说明 pg_roles 里没有当前角色（几乎不可能），当成探针失败而不是默认可信。
		return identity, fmt.Errorf("rls: 未取到当前连接角色（session_user/current_user 为空）")
	}
	return identity, nil
}
