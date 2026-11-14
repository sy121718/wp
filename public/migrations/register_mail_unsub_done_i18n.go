package migrations

// 754 播种退订成功提示的短 key；守门逐条核对本批两条 (item_key, lang)。
func init() {
	registerSeed(Seed{
		Version:   "754-mail-unsub-done-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (('mailUnsubDone','zh-CN'), ('mailUnsubDone','en-US'))`,
		SQL: mustSQL("754_mail_unsub_done_i18n.sql"),
	})
}
