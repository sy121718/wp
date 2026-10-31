package migrations

import "sync"

func registerDropPageSchedule() {
	registerDropPageScheduleOnce.Do(registerDropPageScheduleEntries)
}

var registerDropPageScheduleOnce sync.Once

func registerDropPageScheduleEntries() {

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
