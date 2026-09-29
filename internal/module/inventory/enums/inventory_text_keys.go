package inventoryenums

// inventory_text_keys.go — 后台界面**静态文案**的 i18n key（点分新式常量）。
//
// 与 inventory_enums.go 里那批 `MsgXxx = "MsgXxx"` / `ErrXxx = "ErrXxx"` 的老式常量**不同批**：
// 老式常量的值就是常量名本身（sys_i18n 里存同名 key），本文件的常量值一律是 `admin.*` 点分 key，
// 与词条表的 item_key **逐字相等**（词条见迁移 449_i18n_product_inventory_go_texts）。
//
// 为什么收在这里：调用点原先直接写 key 字面量（`tr("admin.inventory.status.active", "启用中")`），
// 「谁在用这个 key」「改词条要动哪几处」都只能靠字符串检索。收到 enums 之后 key 与常量名一一对应；
// **中文兜底仍留在调用点原地**（词条缺失时的回落，不搬进 enums）。
//
// 模板（.jet / .html）不在此列：Jet 里引用不到 Go 常量，那里的 key 必然内联。
//
// 命名：key 去掉 `admin.` 前缀后的语义路径转驼峰，缩写 SKU / ID 全大写。
// **常量值必须逐字等于原字面量** —— 改错一个字符就等于换了 key，词条取不到、页面回落中文。
const (
	// —— 库存管理页 / 仓库页 / 原因字典页 ——
	InventoryTitle                 = "admin.inventory.title"
	InventoryWarehousesTitle       = "admin.inventory.warehouses.title"
	InventoryReasonsTitle          = "admin.inventory.reasons.title"
	InventoryStockNotStocked       = "admin.inventory.stock.notStocked"
	InventoryStockUnlimited        = "admin.inventory.stock.unlimited"
	InventoryCostUnknown           = "admin.inventory.cost.unknown"
	InventoryStatusActive          = "admin.inventory.status.active"
	InventoryStatusDisabled        = "admin.inventory.status.disabled"
	InventoryDirectionIn           = "admin.inventory.direction.in"
	InventoryDirectionOut          = "admin.inventory.direction.out"
	InventoryDirectionAdjust       = "admin.inventory.direction.adjust"
	InventoryAdjustDirectionAdjust = "admin.inventory.adjust.direction.adjust"
	InventoryAdjustDirectionOut    = "admin.inventory.adjust.direction.out"

	// —— 仓库类型与「· 默认仓」后缀 ——
	InventoryWarehouseTypeSelf       = "admin.inventory.warehouse.type.self"
	InventoryWarehouseTypeThirdParty = "admin.inventory.warehouse.type.third_party"
	InventoryWarehouseTypeVirtual    = "admin.inventory.warehouse.type.virtual"
	// InventoryChangeWarehouseDefaultSuffix 商品列表页与库存页共用的后缀词条（同一份词条两处取用）。
	InventoryChangeWarehouseDefaultSuffix = "admin.inventory.change.warehouseDefaultSuffix"

	// —— 货源页（key 前缀是 `admin.inventory_sources.*`，与 `admin.inventory.*` 不同族）——
	InventorySourcesRelatedTypeDefault = "admin.inventory_sources.relatedTypeDefault"
	InventorySourcesTypeInternal       = "admin.inventory_sources.type.internal"
	InventorySourcesStatsExternal      = "admin.inventory_sources.stats.external"
	InventorySourcesStatsRelated       = "admin.inventory_sources.stats.related"
	InventorySourcesStatsUnrelated     = "admin.inventory_sources.stats.unrelated"

	// —— 采购入库页（key 前缀是 `admin.inventory_purchases.*`）——
	InventoryPurchasesStatusPending   = "admin.inventory_purchases.status.pending"
	InventoryPurchasesStatusPartial   = "admin.inventory_purchases.status.partial"
	InventoryPurchasesColReceived     = "admin.inventory_purchases.col.received"
	InventoryPurchasesKindPurchase    = "admin.inventory_purchases.kind.purchase"
	InventoryPurchasesProductionOpen  = "admin.inventory_purchases.production.open"
	InventoryPurchasesOptionCostLabel = "admin.inventory_purchases.option.costLabel"
	InventoryPurchasesOptionCostUnset = "admin.inventory_purchases.option.costUnset"
)
