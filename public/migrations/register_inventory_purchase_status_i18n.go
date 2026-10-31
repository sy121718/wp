package migrations

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
