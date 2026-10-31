package migrations

import "sync"

func registerProductOutbox() {
	registerProductOutboxOnce.Do(registerProductOutboxSQL)
}

var registerProductOutboxOnce sync.Once

func registerProductOutboxSQL() {

	register(Migration{
		Version:   "309-product-outbox-events",
		TableName: "product_outbox_events",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END " +
			"FROM pg_index x " +
			"JOIN pg_class c ON c.oid = x.indrelid " +
			"JOIN pg_namespace n ON n.oid = c.relnamespace " +
			"JOIN pg_class ic ON ic.oid = x.indexrelid " +
			"WHERE n.nspname = current_schema() AND c.relname = ? " +
			"AND ic.relname = 'idx_product_outbox_pending' " +
			"AND x.indpred IS NOT NULL",
		SQL: mustSQL("309_product_outbox_events.sql"),
	})
}
