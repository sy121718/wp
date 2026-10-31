package migrations

import "sync"

var registerOrderFulfillmentOnce sync.Once

func init() { registerOrderFulfillment() }

func registerOrderFulfillment() {
	registerOrderFulfillmentOnce.Do(func() {
		register(Migration{
			Version:   "652-order-fulfillment-status",
			TableName: "orders",

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

		registerSeed(Seed{
			Version:   "653-order-fulfillment-backfill",
			TableName: "orders",
			ConditionSQL: `SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM orders
				WHERE fulfillment_status = 'pending' AND status <> 'pending'`,
			SQL: mustSQL("653_order_fulfillment_backfill.sql"),
		})
	})
}
