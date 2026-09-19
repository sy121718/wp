package migrations

import "sync"

// register_i18n_view_permission.go — 文案词条页的只读权限点（迁移 294）。
//
// 与 178（写权限点 i18n:manage）同一主题的补口：178 有意让 GET 不挂 Casbin，
// 本迁移把这条口径补齐 —— 只读页面同样要有权限门，菜单隐藏不是访问控制。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册）。
func registerI18nViewPermission() {
	registerI18nViewPermissionOnce.Do(registerI18nViewPermissionSQL)
}

// registerI18nViewPermissionOnce 让重复调用成为空操作（不会重复 register）。
var registerI18nViewPermissionOnce sync.Once

// registerI18nViewPermissionSQL 注册 294（真正干活的那一半，被 Once 包一层）。
func registerI18nViewPermissionSQL() {
	// 294：1 个权限点 + 1 条超管策略 + 1 条菜单绑定调整。
	//
	// 判定必须把权限点代码写进 **SQL 字面量**：CheckSQL 里的 ? 由迁移器传的是**表名**，
	// 写成 permission_code = ? 等于永远查不到行，于是这条迁移每次启动都重跑
	// （178 踩过这个坑：被 SQL 里的 WHERE NOT EXISTS 幂等性掩盖了很久）。
	// ? 仍保留在「表存在」这一项上 —— 表都没了就不该算完成。
	register(Migration{
		Version:   "294-i18n-view-permission",
		TableName: "sys_permission",
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NOT NULL AND (SELECT COUNT(*) FROM sys_permission " +
			"WHERE permission_code = 'i18n:view') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("294_i18n_view_permission.sql"),
	})
}
