package migrations

// register_inventory_purchase_status_i18n.go — 670b：采购单七态的词条（配合 670）。
//
// 门槛逐条枚举本批的 23 个 key（中英共 46 行）。两条硬要求：
//
//   · **点名每个 key** —— 用 LIKE 或「全库总量」判，存量库里随便一批词条都能满足，
//     缺的词永远补不上（AGENTS.md §数据库「seed 门槛必须逐条枚举」）；
//   · **两种语言都数** —— ConditionSQL 返回 >0 即「已完成 → 跳过 SQL」，只数一种语言时
//     另一种缺行仍为 1，于是永远跳过（recent_write_ui_seed_guard_test.go 的定则）。
//
// 本批是**改值 + 新增**混在一起（状态措辞换了），SQL 用 ON CONFLICT DO UPDATE：
// 判据只判「这 46 个组合在不在」，运营改过值也满足 —— 已改的值不会被重新覆盖，
// 只有缺行会被补齐。

import "sync"

var registerInventoryPurchaseStatusI18nOnce sync.Once

func init() { registerInventoryPurchaseStatusI18n() }

func registerInventoryPurchaseStatusI18n() {
	registerInventoryPurchaseStatusI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "670b-inventory-purchase-status-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT CASE WHEN COUNT(*) >= 46 THEN 1 ELSE 0 END FROM sys_i18n
				WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (
					'admin.inventory_purchases.create.asDraft',
					'admin.inventory_purchases.exception',
					'admin.inventory_purchases.exceptionConfirm',
					'admin.inventory_purchases.intro.tail',
					'admin.inventory_purchases.place',
					'admin.inventory_purchases.placeConfirm',
					'admin.inventory_purchases.receipt.submitAll',
					'admin.inventory_purchases.receipt.submitHint',
					'admin.inventory_purchases.resume',
					'admin.inventory_purchases.resumeConfirm',
					'admin.inventory_purchases.status.draft',
					'admin.inventory_purchases.status.exception',
					'admin.inventory_purchases.status.finished',
					'admin.inventory_purchases.status.ordered',
					'admin.inventory_purchases.status.partial',
					'admin.inventory_purchases.status.received',
					'ErrPurchaseStatusInvalid',
					'ErrPurchaseStatusNotCancelable',
					'ErrPurchaseStatusNotExceptionable',
					'ErrPurchaseStatusNotFinishable',
					'ErrPurchaseStatusNotPlaceable',
					'ErrPurchaseStatusNotReceivable',
					'ErrPurchaseStatusNotResumable'
				)`,
			SQL: mustSQL("670b_inventory_purchase_status_i18n.sql"),
		})
	})
}
