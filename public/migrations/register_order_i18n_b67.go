package migrations

// register_order_i18n_b67.go — 654 / 655：B6 / B7 的界面词条（履约状态 + 三屏新增列）。
//
// seed 门槛必须逐条枚举本批 key（禁用 LIKE / 禁用全库总量）：
// 存量库的 sys_i18n 有几万行，用「全表计数 >= N」当门槛，补词条的迁移在存量库上
// 永远判为「已完成」、一条也不会执行 —— 那是迁移 494 记下的坑。

import "sync"

var registerOrderI18nB67Once sync.Once

func init() { registerOrderI18nB67() }

func registerOrderI18nB67() {
	registerOrderI18nB67Once.Do(func() {
		// 654：12 个履约状态 × 中英 = 24 行。
		registerSeed(Seed{
			Version:      "654-order-fulfillment-i18n",
			TableName:    "sys_i18n",
			ConditionSQL: "SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (" + fulfillmentI18nKeys + ")",
			SQL:          mustSQL("654_order_fulfillment_i18n.sql"),
		})

		// 655：待发货页 10 条 + 退货页 4 条 + 订单列表 3 条，中英成对。
		registerSeed(Seed{
			Version:      "655-order-warehouse-pages-i18n",
			TableName:    "sys_i18n",
			ConditionSQL: "SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (" + orderPagesI18nKeys + ")",
			SQL:          mustSQL("655_order_warehouse_pages_i18n.sql"),
		})
	})
}

// fulfillmentI18nKeys 654 seed 覆盖的 key（门槛按 key 数计，12 条）。
const fulfillmentI18nKeys = "'admin.orders.fulfillment.pending', 'admin.orders.fulfillment.paid'," +
	" 'admin.orders.fulfillment.partial', 'admin.orders.fulfillment.shipped'," +
	" 'admin.orders.fulfillment.received', 'admin.orders.fulfillment.returned'," +
	" 'admin.orders.fulfillment.return_done', 'admin.orders.fulfillment.reship'," +
	" 'admin.orders.fulfillment.exception', 'admin.orders.fulfillment.failed'," +
	" 'admin.orders.fulfillment.cancelled', 'admin.orders.fulfillment.refunded'"

// orderPagesI18nKeys 655 seed 覆盖的 key（17 条）。
const orderPagesI18nKeys = "'admin.order.pendingShip.filter.keyword', 'admin.order.pendingShip.filter.keywordPh'," +
	" 'admin.order.pendingShip.col.source', 'admin.order.pendingShip.col.lines'," +
	" 'admin.order.pendingShip.blocked.hint', 'admin.order.pendingShip.blocked.variantMissing'," +
	" 'admin.order.pendingShip.blocked.unmatched', 'admin.order.pendingShip.blocked.warehouseMissing'," +
	" 'admin.order.pendingShip.blocked.stockInsufficient', 'admin.order.pendingShip.blocked.unknown'," +
	" 'admin.returns.col.warehouse', 'admin.returns.col.pending_lines'," +
	" 'admin.returns.detail.warehouse', 'admin.returns.detail.pending_lines'," +
	" 'admin.orders.col.fulfillment', 'admin.orders.col.created_via'," +
	" 'admin.orders.filter.created_via_all'"
