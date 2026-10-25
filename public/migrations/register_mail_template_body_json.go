package migrations

import "sync"

// register_mail_template_body_json.go — 622：模板可视化正文的三列。
//
// 判据是「三列都在」而不是「表存在」：mail_templates 从 129 起就在，只看表名会让这条
// 迁移对既有库永久跳过（那正是「写了列却没加上」的静默失败形态）。
//
// CheckSQL 里的 `?` 由迁移器传入**表名**，且只传这一个参数 —— 列名必须写进 SQL 字面量。
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
