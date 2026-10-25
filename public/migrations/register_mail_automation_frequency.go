package migrations

import "sync"

// register_mail_automation_frequency.go — 623：触发频率与「回信即退出」两列 + 频率闸门索引。
//
// 判据是「两列 + 索引都在」而不是「表存在」：mail_automations 从 131 起就在，
// 只看表名会让这条迁移对既有库永久跳过。
func registerMailAutomationFrequency() {
	registerMailAutomationFrequencyOnce.Do(registerMailAutomationFrequencyMigration)
}

var registerMailAutomationFrequencyOnce sync.Once

func registerMailAutomationFrequencyMigration() {
	register(Migration{
		Version:   "623-mail-automation-frequency",
		TableName: "mail_automations",
		CheckSQL: "SELECT CASE WHEN (" +
			"(SELECT COUNT(*) FROM information_schema.columns " +
			" WHERE table_schema = current_schema() AND table_name = ? " +
			" AND column_name IN ('trigger_frequency', 'exit_on_reply')) = 2 " +
			"AND EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = current_schema() " +
			" AND tablename = 'mail_automation_runs' " +
			" AND indexname = 'idx_mail_automation_runs_automation_contact') " +
			") THEN 1 ELSE 0 END",
		SQL: mustSQL("623_mail_automation_frequency.sql"),
	})
}
