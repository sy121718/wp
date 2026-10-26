package migrations

// register_inventory_moves_open_ledger_i18n.go — 650 的注册项。
//
// 判据判的是「三条词条的值都是本批写的那一句」而不是「行存在」：
// 650 有两条是**改值**，只判行存在的话，值没被改回来的老库会被判成已完成 ——
// 那是这条种子唯一会出错的方向。

import "sync"

func init() { registerInventoryMovesOpenLedgerI18n() }

var inventoryMovesOpenLedgerI18nOnce sync.Once

func registerInventoryMovesOpenLedgerI18n() {
	inventoryMovesOpenLedgerI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "650-inventory-moves-open-ledger-i18n",
			TableName: "sys_i18n",
			ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND (" +
				"(item_key = 'admin.inventory.moves.openLedger' AND item_value = '查看全部流水') OR " +
				"(item_key = 'admin.inventory.moves.hint' AND item_value = '每一次真源变动都留痕：方向、数量、变动前后值、原因、来源引用与批次号。') OR " +
				"(item_key = 'admin.inventory.sku.empty' AND item_value = '选一个 SKU 后，这里列出它在各仓的库存记录（一条货的身份是「仓库 + 仓库 SKU」，同一个仓里可以有多条货）。'))",
			SQL: mustSQL("650_inventory_moves_open_ledger_i18n.sql"),
		})
	})
}
