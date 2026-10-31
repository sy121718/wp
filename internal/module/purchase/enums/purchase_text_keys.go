package purchaseenums

const (
	KeyStatusActive   = "admin.inventory.status.active"
	KeyStatusDisabled = "admin.inventory.status.disabled"

	KeyWarehouseDefaultSuffix = "admin.inventory.change.warehouseDefaultSuffix"

	KeySourcesRelatedTypeDefault = "admin.inventory_sources.relatedTypeDefault"
	KeySourcesTypeInternal       = "admin.inventory_sources.type.internal"
	KeySourcesStatsExternal      = "admin.inventory_sources.stats.external"
	KeySourcesStatsRelated       = "admin.inventory_sources.stats.related"
	KeySourcesStatsUnrelated     = "admin.inventory_sources.stats.unrelated"

	KeyPurchasesStatusDraft     = "admin.inventory_purchases.status.draft"
	KeyPurchasesStatusOrdered   = "admin.inventory_purchases.status.ordered"
	KeyPurchasesStatusPartial   = "admin.inventory_purchases.status.partial"
	KeyPurchasesStatusReceived  = "admin.inventory_purchases.status.received"
	KeyPurchasesStatusException = "admin.inventory_purchases.status.exception"
	KeyPurchasesColReceived     = "admin.inventory_purchases.col.received"
	KeyPurchasesColOutstanding  = "admin.inventory_purchases.col.outstanding"
	KeyPurchasesKindPurchase    = "admin.inventory_purchases.kind.purchase"
	KeyPurchasesProductionOpen  = "admin.inventory_purchases.production.open"
	KeyPurchasesOptionCostLabel = "admin.inventory_purchases.option.costLabel"
	KeyPurchasesStatusFinished  = "admin.inventory_purchases.status.finished"
	KeyPurchasesStatusCancelled = "admin.inventory_purchases.status.cancelled"
	KeyPurchasesOptionCostUnset = "admin.inventory_purchases.option.costUnset"

	KeyBulkSourcePartial = "admin.inventory.bulk.sourcePartial"
	KeyBulkSourceDone    = "admin.inventory.bulk.sourceDone"
)

var ErrDetailFallbacks = map[string]string{}
