package migrations

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
