package migrations

import "sync"

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
