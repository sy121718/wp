package migrations

import "sync"

func registerMailTemplateBodyJSON() {
	registerMailTemplateBodyJSONOnce.Do(registerMailTemplateBodyJSONMigration)
}

var registerMailTemplateBodyJSONOnce sync.Once

func registerMailTemplateBodyJSONMigration() {
	register(Migration{
		Version:   "622-mail-template-body-json",
		TableName: "mail_templates",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END " +
			"FROM information_schema.columns c " +
			"WHERE c.table_schema = current_schema() AND c.table_name = ? " +
			"AND c.column_name IN ('body_json', 'body_compiled_html', 'body_compiled_text')",
		SQL: mustSQL("622_mail_template_body_json.sql"),
	})
}
