package migrations

import "sync"

// register_drop_page_schedule.go — 672 / 672b：定时上下线（PIPE-7）整体下线的 DB 侧收尾。
//
// 见 672_drop_page_schedules.sql 与 672b_retire_page_schedule_i18n.sql 的文件头。两半分开注册是刻意的：
//
//	· 672 是结构变更（DROP 表 + 删三个权限点与三条策略），必须在 **Migrations** 台账；
//	· 672b 是数据清理（sys_i18n 词条），必须在 **Seeds** 台账且版本号大于 460a ——
//	  否则同一轮 RunSeeds 里会「先删后插」，净结果是这 70 行又回来了（636b / 458 的实测形态）。
//
// 判据都写成「删除完成后才成立」（与 636b / 458 同一取舍：宁可重跑，不可静默跳过）——
// 半成品库（删了一半）会重跑，而重复执行只是多跑一次幂等 DELETE。
func registerDropPageSchedule() {
	registerDropPageScheduleOnce.Do(registerDropPageScheduleEntries)
}

// registerDropPageScheduleOnce 让重复调用成为空操作。
var registerDropPageScheduleOnce sync.Once

// registerDropPageScheduleEntries 注册 672（结构）与 672b（数据）。
func registerDropPageScheduleEntries() {
	// 672 的完成判据：表已不存在，且三个权限码与三条策略一条不剩。
	// 不用「表存在即跳过」的默认形态：那会让本迁移在「表删了、权限点没删」的半成品库上被静默跳过。
	//
	// CheckSQL 由迁移器 db.Raw 直接执行、没有任何参数替换：要判定的名字只能写在 SQL 字面量里
	//（TableName 只进台账，写成名字 = ? 永远查不到行）。
	register(Migration{
		Version:   "672-drop-page-schedules",
		TableName: "page_schedules",
		CheckSQL: "SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM sys_casbin_rule " +
			"WHERE v1 IN ('/api/page/schedule/set', '/api/page/schedule/cancel', '/api/page/schedule/list')" +
			") + (" +
			"SELECT COUNT(*) FROM sys_permission " +
			"WHERE permission_code IN ('page:schedule_set', 'page:schedule_cancel', 'page:schedule_list')" +
			") + (CASE WHEN to_regclass('page_schedules') IS NULL THEN 0 ELSE 1 END) = 0 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("672_drop_page_schedules.sql"),
	})

	// 672b 的完成判据：35 个 key 的中英 70 行一行不剩。
	registerSeed(Seed{
		Version:   "672b-retire-page-schedule-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'ErrScheduleActionInvalid', 'ErrScheduleApplyFailed', 'ErrScheduleInPast', " +
			"'ErrScheduleNotFound', 'ErrScheduleOccupied', 'ErrScheduleRunning', " +
			"'MsgScheduleCanceled', 'MsgScheduleSet', " +
			"'admin.pages.action.schedule', 'admin.pages.status.schedule_failed', 'admin.pages.status.scheduled', " +
			"'admin.page.schedule.action', 'admin.page.schedule.action.offline', 'admin.page.schedule.action.publish', " +
			"'admin.page.schedule.cancel', 'admin.page.schedule.col.action', 'admin.page.schedule.col.actions', " +
			"'admin.page.schedule.col.lang', 'admin.page.schedule.col.note', 'admin.page.schedule.col.status', " +
			"'admin.page.schedule.col.time', 'admin.page.schedule.empty', 'admin.page.schedule.lang', " +
			"'admin.page.schedule.lang_hint', 'admin.page.schedule.note', 'admin.page.schedule.redirect', " +
			"'admin.page.schedule.redirect_hint', 'admin.page.schedule.status.canceled', 'admin.page.schedule.status.done', " +
			"'admin.page.schedule.status.failed', 'admin.page.schedule.status.pending', 'admin.page.schedule.status.running', " +
			"'admin.page.schedule.submit', 'admin.page.schedule.time', 'admin.page.schedule.title'" +
			")",
		SQL: mustSQL("672b_retire_page_schedule_i18n.sql"),
	})
}

func init() { registerDropPageSchedule() }
