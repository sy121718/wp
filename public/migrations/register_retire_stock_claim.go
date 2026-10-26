package migrations

// register_retire_stock_claim.go — 638 的注册项（认领概念下线）。
//
// 判据不用默认的「表存在即跳过」：inventory_stocks 一直都在，用默认判据会让这条迁移
// 在存量库上被永久跳过 —— 词条与索引都留着。改成「本批词条 + 索引全部消失」才置位。

import "sync"

func init() { registerRetireStockClaim() }

var retireStockClaimOnce sync.Once

// retireStockClaimI18nKeys 本批删除的词条（与 SQL 里的清单逐字一致）。
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
			// 三件事同时成立才算完成：词条清零、未认领索引消失、skus 页面不再有路由。
			// 只用其中一件会让别的两件被静默跳过（这条迁移的全部内容就是「删干净」）。
			CheckSQL: "SELECT CASE WHEN (" +
				"SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (" + quoted + ")" +
				") + (" +
				"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'idx_inventory_stocks_unclaimed'" +
				") = 0 THEN 1 ELSE 0 END",
			SQL: mustSQL("638_retire_stock_claim.sql"),
		})
	})
}
