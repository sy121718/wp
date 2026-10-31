package migrations

import "sync"

func init() { registerRetireStockClaim() }

var retireStockClaimOnce sync.Once

var retireStockClaimI18nKeys = []string{
	"ErrStockAlreadyClaimed", "ErrStockHasQuantity", "ErrStockNotClaimed",
	"admin.inventory.skus.title", "admin.inventory.skus.lead", "admin.inventory.skus.mid",
	"admin.inventory.skus.boundary", "admin.inventory.skus.createTitle", "admin.inventory.skus.create",
	"admin.inventory.skus.listLead", "admin.inventory.skus.col.claimed", "admin.inventory.skus.col.reserved",
	"admin.inventory.skus.col.available", "admin.inventory.skus.col.cost",
	"admin.inventory.skus.filter.keyword", "admin.inventory.skus.filter.keywordPh",
	"admin.inventory.skus.emptyHeading", "admin.inventory.skus.emptyDesc",
	"admin.inventory.skus.unclaimConfirm", "admin.inventory.skus.unclaim", "admin.inventory.skus.claim",
	"admin.inventory.skus.claimTitle", "admin.inventory.skus.claimTarget", "admin.inventory.skus.claimPick",
	"admin.inventory.skus.claimSubmit", "admin.inventory.skus.createWarehousePh",
	"admin.inventory.skus.createCodePh", "admin.inventory.skus.costPh", "admin.inventory.skus.createSubmit",
	"admin.inventory.stock.claimed", "admin.inventory.stock.unclaimed",
	"admin.inventory.skus.filter.submit",
}

func registerRetireStockClaim() {
	retireStockClaimOnce.Do(func() {
		quoted := ""
		for i, key := range retireStockClaimI18nKeys {
			if i > 0 {
				quoted += ", "
			}
			quoted += "'" + key + "'"
		}
		register(Migration{
			Version:   "638-retire-stock-claim",
			TableName: "sys_i18n",

			CheckSQL: "SELECT CASE WHEN (" +
				"SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (" + quoted + ")" +
				") + (" +
				"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'idx_inventory_stocks_unclaimed'" +
				") = 0 THEN 1 ELSE 0 END",
			SQL: mustSQL("638_retire_stock_claim.sql"),
		})
	})
}
