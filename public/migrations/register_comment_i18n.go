package migrations

// register_comment_i18n.go — 迁移 466a 的 seed 注册（BIZ-5 的文案词条）。
//
// 门槛判据**枚举本批自己的 70 个 item_key**（上界封闭，70 × 2 语言 = 140 行）：
// 用 LIKE 前缀会让「别批次已有同前缀行」把计数抬高、本批被静默跳过（058 的真实故障），
// 也会在将来新增同前缀 key 时永远追不平、每次启动都重跑（076 的真实故障）。
// （`site.fragment.comment.*` 与 `admin.comment.*` 两个前缀下本批是唯一写入者；
// 将来若有人续写同前缀，重跑也会被 ON CONFLICT 挡住。）
//
// 注册方式：本文件自带 func init()（与 464 / 462a / 455 一致），不在 register.go 的 init()
// 里再加一行。
func init() {
	registerSeed(Seed{
		Version:   "466a-comment-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 140 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.comment.action.approve', 'admin.comment.action.reject', 'admin.comment.col.actions', " +
			"'admin.comment.col.author', 'admin.comment.col.body', 'admin.comment.col.entity', " +
			"'admin.comment.col.entity_id', 'admin.comment.col.status', 'admin.comment.col.time', " +
			"'admin.comment.confirmReject', 'admin.comment.done.approved', 'admin.comment.done.rejected', " +
			"'admin.comment.empty', 'admin.comment.empty_hint', 'admin.comment.err.nothing_selected', " +
			"'admin.comment.filter.all', 'admin.comment.filter.entity_type', 'admin.comment.filter.keyword', " +
			"'admin.comment.filter.reset', 'admin.comment.filter.search', 'admin.comment.filter.status', " +
			"'admin.comment.hint', 'admin.comment.lastError', 'admin.comment.load_failed', " +
			"'admin.comment.menu', 'admin.comment.no_project', 'admin.comment.project_required', " +
			"'admin.comment.replyBadge', 'admin.comment.status.approved', 'admin.comment.status.pending', " +
			"'admin.comment.status.rejected', 'admin.comment.status.spam', 'comment.err.bodyRequired', " +
			"'comment.err.bodyTooLong', 'comment.err.csrfRequired', 'comment.err.entityIDInvalid', " +
			"'comment.err.entityTypeUnknown', 'comment.err.internal', 'comment.err.invalidParam', " +
			"'comment.err.loginRequired', 'comment.err.notAllowed', 'comment.err.nothingSelected', " +
			"'comment.err.parentInvalid', 'comment.err.projectRequired', 'comment.err.rateLimited', " +
			"'comment.err.unavailable', 'comment.msg.reviewApproved', 'comment.msg.reviewRejected', " +
			"'comment.msg.submitPending', 'site.fragment.comment.author_guest', 'site.fragment.comment.body_placeholder', " +
			"'site.fragment.comment.body_required', 'site.fragment.comment.body_too_long', 'site.fragment.comment.csrf_expired', " +
			"'site.fragment.comment.empty', 'site.fragment.comment.entity_invalid', 'site.fragment.comment.entity_missing', " +
			"'site.fragment.comment.form_title', 'site.fragment.comment.guest', 'site.fragment.comment.list_failed', " +
			"'site.fragment.comment.more', 'site.fragment.comment.pending_notice', 'site.fragment.comment.project_missing', " +
			"'site.fragment.comment.rate_limited', 'site.fragment.comment.reply', 'site.fragment.comment.reply_to', " +
			"'site.fragment.comment.submit', 'site.fragment.comment.submit_failed', 'site.fragment.comment.title', " +
			"'site.fragment.comment.unavailable'" +
			")",
		SQL: mustSQL("466a_comment_i18n.sql"),
	})
}
