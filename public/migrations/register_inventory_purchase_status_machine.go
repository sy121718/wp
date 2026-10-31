package migrations

import "sync"

var registerInventoryPurchaseStatusMachineOnce sync.Once

func init() { registerInventoryPurchaseStatusMachine() }

func registerInventoryPurchaseStatusMachine() {
	registerInventoryPurchaseStatusMachineOnce.Do(func() {
		register(Migration{
			Version:   "670-inventory-purchase-status-machine",
			TableName: "inventory_purchase_orders",
			CheckSQL: `SELECT CASE WHEN (
				SELECT COUNT(*) FROM pg_constraint
				WHERE conrelid = to_regclass('inventory_purchase_orders')
				  AND conname = 'inventory_purchase_orders_status_check'
				  AND pg_get_constraintdef(oid) LIKE '%draft%'
				  AND pg_get_constraintdef(oid) LIKE '%exception%'
			) = 1 AND (
				SELECT COUNT(*) FROM pg_proc
				WHERE proname = 'fn_inventory_purchase_order_status_guard'
				  AND prosrc LIKE '%''ordered'', ''partial'', ''received''%'
			) = 1 AND (
				SELECT COUNT(*) FROM inventory_purchase_orders WHERE status = 'pending'
			) = 0 THEN 1 ELSE 0 END`,
			SQL: mustSQL("670_inventory_purchase_status_machine.sql"),
		})
	})
}
