package purchaseenums

// purchase_text_keys.go — 采购域后台界面**静态文案**的 i18n key。
//
// 与 purchase_enums.go 里那批 `MsgXxx = "MsgXxx"` / `ErrXxx = "ErrXxx"` 的老式常量**不同批**：
// 老式常量的值就是常量名本身（sys_i18n 里存同名 key），本文件的常量值一律是 `admin.*` 点分 key，
// 与词条表的 item_key **逐字相等**。
//
// **值逐一照搬自 inventoryenums**（2026-10 采购独立成模块）：sys_i18n 里的词条按 key
// 逐字匹配，改一个字符就等于让已发布的译文全部失配、界面回落中文兜底。常量名统一加
// `Key` 前缀以区分「值常量」（StatusActive = "active"）与「文案 key 常量」。
//
// 中文兜底仍留在调用点原地（词条缺失时的回落，不搬进 enums）。
const (
	// —— 启停状态 ——
	KeyStatusActive   = "admin.inventory.status.active"
	KeyStatusDisabled = "admin.inventory.status.disabled"

	// —— 「· 默认仓」后缀（与库存页 / 商品页共用同一份词条）——
	KeyWarehouseDefaultSuffix = "admin.inventory.change.warehouseDefaultSuffix"

	// —— 货源页（key 前缀是 `admin.inventory_sources.*`）——
	KeySourcesRelatedTypeDefault = "admin.inventory_sources.relatedTypeDefault"
	KeySourcesTypeInternal       = "admin.inventory_sources.type.internal"
	KeySourcesStatsExternal      = "admin.inventory_sources.stats.external"
	KeySourcesStatsRelated       = "admin.inventory_sources.stats.related"
	KeySourcesStatsUnrelated     = "admin.inventory_sources.stats.unrelated"

	// —— 采购入库页（key 前缀是 `admin.inventory_purchases.*`）——
	KeyPurchasesStatusPending   = "admin.inventory_purchases.status.pending"
	KeyPurchasesStatusPartial   = "admin.inventory_purchases.status.partial"
	KeyPurchasesColReceived     = "admin.inventory_purchases.col.received"
	KeyPurchasesKindPurchase    = "admin.inventory_purchases.kind.purchase"
	KeyPurchasesProductionOpen  = "admin.inventory_purchases.production.open"
	KeyPurchasesOptionCostLabel = "admin.inventory_purchases.option.costLabel"
	KeyPurchasesStatusFinished  = "admin.inventory_purchases.status.finished"
	KeyPurchasesStatusCancelled = "admin.inventory_purchases.status.cancelled"
	KeyPurchasesOptionCostUnset = "admin.inventory_purchases.option.costUnset"

	// —— 批量删除货源的结论（失败出口的事实形态：key + 中文兜底 + `%s` 参数）——
	KeyBulkSourcePartial = "admin.inventory.bulk.sourcePartial"
	KeyBulkSourceDone    = "admin.inventory.bulk.sourceDone"
)

// ErrDetailFallbacks 明细词条的中文兜底（i18n 未初始化 / 该 key 没有词条时用）。
//
// 目前采购域没有自己的明细词条 —— 表在这里是为了让出口的**接口**与库存侧同形：
// 出口按 key 查表、未登记就跳过该段并记日志，将来加了明细词条只需补这里一行。
var ErrDetailFallbacks = map[string]string{}
