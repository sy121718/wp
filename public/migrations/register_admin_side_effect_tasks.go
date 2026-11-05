package migrations

import "sync"

// registerAdminSideEffectTasks 登记 admin 域持久副作用任务表（705）。
//
// 这张表存的是「业务已提交、但副作用还没确认生效」的持久事实：策略重载与会话吊销
// 跨库/依赖进程内状态，无法和 PG 业务事务原子提交。任务行与业务同事务写入，
// 提交后由 admin 自己的 worker 重放，失败保留 attempts / last_error。
func registerAdminSideEffectTasks() {
	registerAdminSideEffectTasksOnce.Do(registerAdminSideEffectTasksMigration)
}

var registerAdminSideEffectTasksOnce sync.Once

func registerAdminSideEffectTasksMigration() {
	register(Migration{
		Version:   "705-admin-side-effect-tasks",
		TableName: "admin_side_effect_tasks",
		SQL:       mustSQL("705_admin_side_effect_tasks.sql"),
	})
}
