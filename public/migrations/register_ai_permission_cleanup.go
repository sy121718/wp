package migrations

import "sync"

// register_ai_permission_cleanup.go — AI 陈旧权限行清理（518）。
func registerAIStalePermissionCleanup() {
	registerAIStalePermissionCleanupOnce.Do(registerAIStalePermissionCleanupSeed)
}

// registerAIStalePermissionCleanupOnce 让重复调用成为空操作。
var registerAIStalePermissionCleanupOnce sync.Once

// registerAIStalePermissionCleanupSeed 注册 518。
//
// 判定只枚举本批自己的对象（上界封闭）：本批不建表、不 seed 词条，只删 AI 早期
// 「一码多路由」留下的两个死权限码（ai:view / ai:manage）及其策略行，所以门槛就是
// 「这两个码都不存在」—— 成立即视为本批已完成，整条跳过。
//
// 先判 to_regclass：权限表由更早的迁移建出，但本迁移不能假设它一定在（例如只跑过
// 一部分迁移的库），表不存在时也应当整批跳过而不是报错。
//
// ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，值只能写进 SQL 字面量。
func registerAIStalePermissionCleanupSeed() {
	registerSeed(Seed{
		Version:   "518-ai-stale-permission-cleanup",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE" +
			" WHEN to_regclass('sys_permission') IS NULL THEN 1" +
			" WHEN NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code IN ('ai:view', 'ai:manage')) THEN 1" +
			" ELSE 0 END",
		SQL: mustSQL("518_ai_stale_permission_cleanup.sql"),
	})
}
