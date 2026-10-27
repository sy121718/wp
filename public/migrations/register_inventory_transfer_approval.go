package migrations

// register_inventory_transfer_approval.go — 651：调拨单审批（requested / rejected）。
//
// 判据只查系统目录与约束定义，不引用目标列本身：迁移器先跑 CheckSQL 再决定是否执行，
// 而 MySQL/PG 在**解析阶段**就会对不存在的列报错（637/640/642 都踩过同一个坑）——
// 那会把「尚未完成」误判成一次失败。

import "sync"

var registerInventoryTransferApprovalOnce sync.Once

func init() { registerInventoryTransferApproval() }

func registerInventoryTransferApproval() {
	registerInventoryTransferApprovalOnce.Do(func() {
		register(Migration{
			Version:   "651-inventory-transfer-approval",
			TableName: "inventory_transfers",
			// 六列齐 + 状态约束已含 rejected + 部分索引在。
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
