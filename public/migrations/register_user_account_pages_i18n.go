package migrations

// register_user_account_pages_i18n.go — 迁移 455 的 seed 注册。
//
// 门槛判据**枚举本批自己的 91 个 key**（上界封闭，91 × 2 语言 = 182 行）：
// 用 LIKE 前缀会让「别批次已有同前缀行」把计数抬高、本批被静默跳过（058 的故障），
// 也会在将来新增同前缀 key 时永远追不平、每次启动都重跑（076 的故障）。
//
// 少一行就重跑 SQL —— SQL 自身 ON CONFLICT (item_key, lang) DO NOTHING，
// 重跑不会覆盖已人工改过的译文。
//
// 注册方式：本文件自带 func init()（与 448 / 453 一致），不必在 register.go 的 init() 里显式调用。
func init() {
	registerSeed(Seed{
		Version:   "455-i18n-user-account-pages",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 182 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('user.field.username', 'user.field.email', 'user.field.nickname', " +
			"'user.field.password', 'user.field.new_password', 'user.field.old_password', " +
			"'user.act.login', 'user.act.register', 'user.act.logout', " +
			"'user.nav.account_center', 'user.footer.service', 'user.hint.password_min8', " +
			"'user.login.sub', 'user.login.account_label', 'user.login.remember', " +
			"'user.login.forgot_link', 'user.login.no_account', 'user.login.go_register', " +
			"'user.register.heading', 'user.register.sub', 'user.register.email_hint', " +
			"'user.register.nickname_label', 'user.register.submit', 'user.register.has_account', " +
			"'user.register.go_login', " +
			"'user.forgot.heading', 'user.forgot.sub', 'user.forgot.submit', " +
			"'user.forgot.remembered', 'user.forgot.back_login', " +
			"'user.register_done.heading', 'user.register_done.sent_prefix', " +
			"'user.register_done.sent_suffix', 'user.register_done.queued', " +
			"'user.register_done.mail_failed', 'user.register_done.resend', " +
			"'user.register_done.no_mail', 'user.register_done.go_login', " +
			"'user.reset.heading', 'user.reset.body_prefix', 'user.reset.body_suffix', " +
			"'user.reset.hint', 'user.reset.submit', " +
			"'user.account.heading', 'user.account.verified', 'user.account.unverified', " +
			"'user.account.registered_at', 'user.account.last_login', " +
			"'user.account.profile_section', 'user.account.gender', 'user.account.gender_unset', " +
			"'user.account.gender_male', 'user.account.gender_female', " +
			"'user.account.first_name', 'user.account.last_name', 'user.account.birthday', " +
			"'user.account.phone', 'user.account.website', 'user.account.company', " +
			"'user.account.bio', 'user.account.address', 'user.account.country', " +
			"'user.account.region', 'user.account.city', 'user.account.postcode', " +
			"'user.account.save_profile', 'user.account.prefs_section', 'user.account.page_size', " +
			"'user.account.profile_visibility', 'user.account.visibility_public', " +
			"'user.account.visibility_members', 'user.account.visibility_private', " +
			"'user.account.timezone', 'user.account.email_notify', 'user.account.sms_notify', " +
			"'user.account.show_online', 'user.account.save_prefs', " +
			"'user.account.sessions_section', 'user.account.unknown_browser', " +
			"'user.account.current_device', 'user.account.ip_unknown', " +
			"'user.account.last_active', 'user.account.signed_in_at', 'user.account.kick', " +
			"'user.account.logout_others', 'user.account.no_other_sessions', " +
			"'user.account.change_password', 'user.account.password_hint', " +
			"'user.message.back_account', 'user.message.go_login', 'user.message.register_new') " +
			"AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("455_i18n_user_account_pages.sql"),
	})
}
