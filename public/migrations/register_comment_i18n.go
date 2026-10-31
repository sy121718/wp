package migrations

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
