package migrations

// 460a — 定时上下线（PIPE-7）的词条：业务错误 / 成功回执 / 后台面板与列表徽标文案。
//
// 与 460（page_schedules 表）**同批**：表负责排定与执行、词条负责失败原因与界面文案，
// 两半一起落库才不会出现「表已上、页面上显示裸 key」的中间态（编号后缀是仓库既有的
// 「同批第二半」写法，见 register.go 的 compareVersion 注释）。
//
// 门槛判据**逐条枚举本批自己的 35 个 item_key**（上界封闭，35 × 2 = 70 行）：
//   - 不用 LIKE 前缀：别的批次已有同前缀行时计数虚高 → 本批被静默跳过（058 的真实故障）；
//   - 不用全库总量：将来新增同前缀 key 时永远追不平 → 每次启动重跑（076 的真实故障）。
//
// 放在**种子**台账（registerSeed 而不是 register）：本批只新增词条、不改任何既有 key，
// 词条属于 seed 语义（可重复写入的默认值）。
//
// 注册方式：本文件自带 init()（与 459 的 register_page_mail_err_detail_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
func init() {
	registerSeed(Seed{
		Version:   "460a-page-schedule-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 70 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			// 业务错误 6 条（page/service/page_errors.go 的 sentinel）。
			"'ErrScheduleNotFound', 'ErrScheduleInPast', 'ErrScheduleActionInvalid', " +
			"'ErrScheduleRunning', 'ErrScheduleOccupied', 'ErrScheduleApplyFailed', " +
			// 成功回执 2 条。
			"'MsgScheduleSet', 'MsgScheduleCanceled', " +
			// 页面列表徽标与入口 3 条（admin/page/pages.html）。
			"'admin.pages.status.scheduled', 'admin.pages.status.schedule_failed', " +
			"'admin.pages.action.schedule', " +
			// 定时上下线面板 19 条（fragments/page_schedule_panel.html）。
			"'admin.page.schedule.title', 'admin.page.schedule.action', " +
			"'admin.page.schedule.action.publish', 'admin.page.schedule.action.offline', " +
			"'admin.page.schedule.time', 'admin.page.schedule.lang', " +
			"'admin.page.schedule.lang_hint', 'admin.page.schedule.redirect', " +
			"'admin.page.schedule.redirect_hint', 'admin.page.schedule.note', " +
			"'admin.page.schedule.submit', 'admin.page.schedule.empty', " +
			"'admin.page.schedule.col.time', 'admin.page.schedule.col.action', " +
			"'admin.page.schedule.col.lang', 'admin.page.schedule.col.status', " +
			"'admin.page.schedule.col.note', 'admin.page.schedule.col.actions', " +
			"'admin.page.schedule.cancel', " +
			// 排定状态 5 条（handler 取词后渲染）。
			"'admin.page.schedule.status.pending', 'admin.page.schedule.status.running', " +
			"'admin.page.schedule.status.done', 'admin.page.schedule.status.failed', " +
			"'admin.page.schedule.status.canceled'" +
			")",
		SQL: mustSQL("460a_page_schedule_i18n.sql"),
	})
}
