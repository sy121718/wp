package migrations

// register_order_fulfillment.go — 652 / 653：订单履约状态独立成列 + 回填（B7）。
//
// 判据只查系统目录（pg_attribute / pg_constraint / pg_indexes），不显式引用
// fulfillment_status 这一列本身：迁移器先跑 CheckSQL 再决定是否执行，而 PG 在**解析阶段**
// 就会对不存在的列报错 —— 那会把「尚未完成」误判成一次失败（637/640/642/651 都踩过）。

import "sync"

var registerOrderFulfillmentOnce sync.Once

func init() { registerOrderFulfillment() }

func registerOrderFulfillment() {
	registerOrderFulfillmentOnce.Do(func() {
		register(Migration{
			Version:   "652-order-fulfillment-status",
			TableName: "orders",
			// 一列齐 + 取值约束在 + 索引在。
			CheckSQL: `SELECT CASE WHEN (
				SELECT COUNT(*) FROM pg_attribute
				WHERE attrelid = to_regclass('orders')
				  AND attname = 'fulfillment_status' AND NOT attisdropped
			) = 1 AND (
				SELECT COUNT(*) FROM pg_constraint
				WHERE conrelid = to_regclass('orders')
				  AND conname = 'ck_orders_fulfillment_status'
				  AND pg_get_constraintdef(oid) LIKE '%return_done%'
			) = 1 AND (
				SELECT COUNT(*) FROM pg_indexes
				WHERE tablename = 'orders' AND indexname = 'idx_orders_project_fulfillment'
			) = 1 THEN 1 ELSE 0 END`,
			SQL: mustSQL("652_order_fulfillment_status.sql"),
		})

		// 653 只做数据回填，没有结构变化：判据是「还有没有停在默认值上、而 status 已经能推导的单」。
		// 一条都不剩即为完成；重复执行结果相同（SET 是幂等的 CASE）。
		registerSeed(Seed{
			Version:   "653-order-fulfillment-backfill",
			TableName: "orders",
			ConditionSQL: `SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM orders
				WHERE fulfillment_status = 'pending' AND status <> 'pending'`,
			SQL: mustSQL("653_order_fulfillment_backfill.sql"),
		})
	})
}
