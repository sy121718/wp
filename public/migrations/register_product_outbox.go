package migrations

import "sync"

// register_product_outbox.go — 商品写路径的静态产物失效 outbox（迁移 309，审计 ARCH-01）。
//
// 表形状与消费者在 internal/module/product（model/product_outbox_model.go 写、
// service/product_outbox.go 消费）；这里只负责建表 + 幂等判定。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册）。
func registerProductOutbox() {
	registerProductOutboxOnce.Do(registerProductOutboxSQL)
}

// registerProductOutboxOnce 让重复调用成为空操作（不会重复 register）。
var registerProductOutboxOnce sync.Once

// registerProductOutboxSQL 注册 309（真正干活的那一半，被 Once 包一层）。
func registerProductOutboxSQL() {
	// 判定「待办部分索引在位」而不是「表存在」：只看表名会让这条迁移对既有库永久跳过。
	// 按 pg_index 读索引定义（而不是拿 pg_indexes.indexdef 做字符串匹配）：indexdef 的
	// 文本随 PG 版本变化，判据会被版本差异骗过去（307 的同一取舍）。
	// CheckSQL 里的 ? 由迁移器传入**表名**，且只传这一个参数（178 的坑）。
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
