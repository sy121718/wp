package migrations

import "sync"

var registerInventoryTransferApprovalOnce sync.Once

func init() { registerInventoryTransferApproval() }

func registerInventoryTransferApproval() {
	registerInventoryTransferApprovalOnce.Do(func() {
		register(Migration{
			Version:   "651-inventory-transfer-approval",
			TableName: "inventory_transfers",

			CheckSQL: `SELECT CASE WHEN (
				SELECT COUNT(*) FROM pg_attribute
				WHERE attrelid = to_regclass('inventory_transfers')
				  AND attname IN ('requested_by', 'approved_by', 'approved_name',
				                  'rejected_by', 'rejected_name', 'rejected_reason')
				  AND NOT attisdropped
			) = 6 AND (
				SELECT COUNT(*) FROM pg_constraint
				WHERE conrelid = to_regclass('inventory_transfers')
				  AND conname = 'ck_inventory_transfers_status'
				  AND pg_get_constraintdef(oid) LIKE '%rejected%'
			) = 1 AND (
				SELECT COUNT(*) FROM pg_indexes
				WHERE tablename = 'inventory_transfers' AND indexname = 'idx_inventory_transfers_requested'
			) = 1 THEN 1 ELSE 0 END`,
			SQL: mustSQL("651_inventory_transfer_approval.sql"),
		})
	})
}
