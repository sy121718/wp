package migrations

func init() {
	registerSeed(Seed{
		Version: "746-mail-batch-rejected-i18n", TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*)=4 THEN 1 ELSE 0 END FROM sys_i18n WHERE (item_key, lang) IN (('mailBatchReject','zh-CN'),('mailBatchReject','en-US'),('mailCsvBad','zh-CN'),('mailCsvBad','en-US'))`,
		SQL:          mustSQL("746_mail_batch_rejected_i18n.sql"),
	})
}
