package migrations

// register_inventory_purchase_status_machine.go — 670：采购单七态状态机。
//
// 判据只查系统目录与约束 / 函数定义，不引用目标列本身：迁移器**先跑 CheckSQL 再决定
// 是否执行**，而 PG 在解析阶段就会对不存在的列 / 约束报错（637/640/642 踩过同一个坑）——
// 那会把「尚未完成」误判成一次失败。
//
// 三条一起判，缺一不可：
//   · 单头状态约束已认 draft 与 exception（拆旧装新的结果）；
//   · 守卫函数已按「推导态名单」放行人工态；
//   · 库里不再有 pending 行（回填完成）。
//
// 只看约束会在「约束改了、回填没跑」的中间态上误判为完成。

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
