package migrations

import "sync"

func registerAIStalePermissionCleanup() {
	registerAIStalePermissionCleanupOnce.Do(registerAIStalePermissionCleanupSeed)
}

var registerAIStalePermissionCleanupOnce sync.Once

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
