package migrations

// registerAIEventCallLogColumns 登记 761：把 529 / 535 / 566 三份 seed 里的观测列
// 收进迁移。seed 只放数据，结构归迁移；三份 seed 已随之退役。
func registerAIEventCallLogColumns() {
	register(Migration{
		Version:   "761-ai-event-call-log-columns",
		TableName: "ai_event",

		CheckSQL: `SELECT CASE WHEN COUNT(*) = 5 THEN 1 ELSE 0 END
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND (table_name, column_name) IN (
			    ('ai_event', 'provider_key'),
			    ('ai_event', 'model_id'),
			    ('ai_event', 'user_id'),
			    ('ai_call_log', 'cached_tokens'),
			    ('ai_call_log', 'cached_reported')
			  )`,
		SQL: mustSQL("761_ai_event_call_log_columns.sql"),
	})
}
