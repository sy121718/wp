package migrations

import "sync"

var registerOrderI18nB67Once sync.Once

func init() { registerOrderI18nB67() }

func registerOrderI18nB67() {
	registerOrderI18nB67Once.Do(func() {

		registerSeed(Seed{
			Version:      "654-order-fulfillment-i18n",
			TableName:    "sys_i18n",
			ConditionSQL: "SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (" + fulfillmentI18nKeys + ")",
			SQL:          mustSQL("654_order_fulfillment_i18n.sql"),
		})

		registerSeed(Seed{
			Version:      "655-order-warehouse-pages-i18n",
			TableName:    "sys_i18n",
			ConditionSQL: "SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (" + orderPagesI18nKeys + ")",
			SQL:          mustSQL("655_order_warehouse_pages_i18n.sql"),
		})
	})
}

const fulfillmentI18nKeys = "'admin.orders.fulfillment.pending', 'admin.orders.fulfillment.paid'," +
	" 'admin.orders.fulfillment.partial', 'admin.orders.fulfillment.shipped'," +
	" 'admin.orders.fulfillment.received', 'admin.orders.fulfillment.returned'," +
	" 'admin.orders.fulfillment.return_done', 'admin.orders.fulfillment.reship'," +
	" 'admin.orders.fulfillment.exception', 'admin.orders.fulfillment.failed'," +
	" 'admin.orders.fulfillment.cancelled', 'admin.orders.fulfillment.refunded'"

const orderPagesI18nKeys = "'admin.order.pendingShip.filter.keyword', 'admin.order.pendingShip.filter.keywordPh'," +
	" 'admin.order.pendingShip.col.source', 'admin.order.pendingShip.col.lines'," +
	" 'admin.order.pendingShip.blocked.hint', 'admin.order.pendingShip.blocked.variantMissing'," +
	" 'admin.order.pendingShip.blocked.unmatched', 'admin.order.pendingShip.blocked.warehouseMissing'," +
	" 'admin.order.pendingShip.blocked.stockInsufficient', 'admin.order.pendingShip.blocked.unknown'," +
	" 'admin.returns.col.warehouse', 'admin.returns.col.pending_lines'," +
	" 'admin.returns.detail.warehouse', 'admin.returns.detail.pending_lines'," +
	" 'admin.orders.col.fulfillment', 'admin.orders.col.created_via'," +
	" 'admin.orders.filter.created_via_all'"
