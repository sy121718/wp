package migrations

func registerOrderAndSiteSlots() {

	register(Migration{
		Version:   "135-order",
		TableName: "orders",

		CheckSQL: allChecks(
			"SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM information_schema.tables "+
				"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) "+
				"AND table_name IN ('orders', 'order_items', 'order_status_logs')",
			coverObjects("orders", "uk_orders_request_id", "idx_orders_project_status", "idx_orders_user", "idx_orders_customer_email", "idx_orders_transaction", "idx_orders_create_time", "order_items", "idx_order_items_order", "idx_order_items_variant", "order_status_logs", "idx_order_status_logs_order"),
		),
		SQL: mustSQL("135_order.sql"),
	})

	registerSeed(Seed{
		Version:   "136-order-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'order:list', 'order:get', 'order:create', 'order:status', " +
			"'order:cancel', 'order:refund', 'order:item_list', 'order:log_list')",
		SQL: mustSQL("136_order_permissions.sql"),
	})

	register(Migration{
		Version: "138-page-site-slots", TableName: "page_site_slots",
		CheckSQL: `SELECT CASE WHEN to_regclass(CAST(? AS text)) IS NOT NULL OR EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid=to_regclass('page_dependencies') AND conname='page_dependencies_dependency_kind_check' AND POSITION('page_target' IN pg_get_constraintdef(oid))>0 AND POSITION('site_slot' IN pg_get_constraintdef(oid))=0) THEN 1 ELSE 0 END`,
		SQL:      mustSQL("138_page_site_slots.sql"),
	})

	registerSeed(Seed{
		Version:   "139-page-site-slot-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'page:site_slot_list', 'page:site_slot_bind', 'page:site_slot_unbind')",
		SQL: mustSQL("139_page_site_slot_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "137-guest-account-template",
		TableName:    "mail_templates",
		ConditionSQL: "SELECT COUNT(*) FROM mail_templates WHERE template_key = 'guest_account'",
		SQL:          mustSQL("137_guest_account_template.sql"),
	})

	register(Migration{
		Version:   "141-order-coupons",
		TableName: "coupons",
		CheckSQL: allChecks(
			"SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.tables "+
				"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) "+
				"AND table_name IN ('coupons', 'coupon_redemptions')",
			coverObjects("coupons", "uq_coupons_project_code", "idx_coupons_project_status", "coupon_redemptions", "uq_coupon_redemptions_coupon_order", "idx_coupon_redemptions_project", "idx_coupon_redemptions_user"),
		),
		SQL: mustSQL("141_order_coupons.sql"),
	})

	registerSeed(Seed{
		Version:   "142-order-coupon-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 7 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'order:coupon_list', 'order:coupon_get', 'order:coupon_create', 'order:coupon_update', " +
			"'order:coupon_delete', 'order:coupon_validate', 'order:coupon_redemption')",
		SQL: mustSQL("142_order_coupon_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "143-order-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_menus WHERE type = 2 AND deleted_at IS NULL AND title IN ('订单管理', '优惠码')",
		SQL:          mustSQL("143_order_menu.sql"),
	})

	register(Migration{
		Version:   "144-order-returns",
		TableName: "order_returns",
		CheckSQL: allChecks(
			"SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.tables "+
				"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) "+
				"AND table_name IN ('order_returns', 'order_return_items')",
			coverObjects("order_returns", "uq_order_returns_no", "uq_order_returns_request", "idx_order_returns_order", "idx_order_returns_project_status", "idx_order_returns_user", "order_return_items", "idx_order_return_items_return", "idx_order_return_items_order_item"),
		),
		SQL: mustSQL("144_order_returns.sql"),
	})

	registerSeed(Seed{
		Version:   "145-order-return-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 5 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'order:return_list', 'order:return_get', 'order:return_approve', " +
			"'order:return_reject', 'order:return_receive')",
		SQL: mustSQL("145_order_return_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "146-order-note-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'order:note'",
		SQL:          mustSQL("146_order_note_permission.sql"),
	})

	register(Migration{
		Version:   "147-page-views",
		TableName: "page_views",
		CheckSQL: allChecks(
			"SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.tables "+
				"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) "+
				"AND table_name = 'page_views'",
			coverObjects("page_views", "idx_page_views_project_time", "idx_page_views_project_path_time"),
		),
		SQL: mustSQL("147_page_views.sql"),
	})

	register(Migration{
		Version:   "256-order-item-cost-nullable-movement-unit-cost",
		TableName: "order_items",
		CheckSQL: "SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name = 'order_items' AND column_name = 'cost_price' " +
			"AND is_nullable = 'YES' AND column_default IS NULL" +
			") = 1 AND (" +
			"SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() " +
			"AND table_name = 'inventory_stock_movements' AND column_name = 'unit_cost'" +
			") = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("256_order_item_cost_nullable.sql"),
	})

	registerSeed(Seed{
		Version:      "270-inventory-stock-sku-required-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = 'ErrStockSKURequired'",
		SQL:          mustSQL("270_inventory_stock_sku_required_i18n.sql"),
	})

	registerSeed(Seed{
		Version:   "272-inventory-production-inbound-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 22 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN (" +
			"'admin.inventory_purchases.production.title', 'admin.inventory_purchases.production.open', " +
			"'admin.inventory_purchases.production.noInternal', 'admin.inventory_purchases.production.labelSource', " +
			"'admin.inventory_purchases.production.optionSource', 'admin.inventory_purchases.production.labelSku', " +
			"'admin.inventory_purchases.production.optionSku', 'admin.inventory_purchases.production.labelQuantity', " +
			"'admin.inventory_purchases.production.labelCost', 'admin.inventory_purchases.production.hintCost', " +
			"'admin.inventory_purchases.production.submit')",
		SQL: mustSQL("272_inventory_production_inbound_i18n.sql"),
	})

	register(Migration{
		Version:   "501-order-ship-country",
		TableName: "orders",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) " +
			"AND table_name = 'orders' AND column_name IN ('ship_country', 'bill_country')",
		SQL: mustSQL("501_order_ship_country.sql"),
	})

	registerSeed(Seed{
		Version:   "504-order-new-country-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN (" +
			"'admin.order_new.field.country', 'admin.order_new.field.country_none')",
		SQL: mustSQL("504_i18n_order_new_country.sql"),
	})

	register(Migration{
		Version:   "627-order-shipment",
		TableName: "order_shipments",
		CheckSQL: allChecks(
			"SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM information_schema.tables "+
				"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) "+
				"AND table_name IN ('order_carriers', 'order_shipments', 'order_shipment_items')",
			coverObjects("order_carriers", "uq_order_carriers_project_code", "order_shipments", "uq_order_shipments_no", "uq_order_shipments_request", "idx_order_shipments_order", "order_shipment_items", "uq_order_shipment_items_row", "idx_order_shipment_items_item"),
		),
		SQL: mustSQL("627_order_shipment.sql"),
	})

	register(Migration{
		Version:   "628-order-item-warehouse",
		TableName: "order_items",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'warehouse_id'",
		SQL: mustSQL("628_order_item_warehouse.sql"),
	})

	registerSeed(Seed{
		Version:   "629-order-shipment-pages-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 118 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.order.pendingShip.title', 'admin.order.pendingShip.hint', 'admin.order.pendingShip.sub', " +
			"'admin.order.pendingShip.listLead', 'admin.order.pendingShip.col.shipmentNo', 'admin.order.pendingShip.col.orderNo', " +
			"'admin.order.pendingShip.col.customer', 'admin.order.pendingShip.col.total', 'admin.order.pendingShip.col.units', " +
			"'admin.order.pendingShip.col.remaining', 'admin.order.pendingShip.col.orderedAt', 'admin.order.pendingShip.emptyHeading', " +
			"'admin.order.pendingShip.emptyDesc', 'admin.order.pendingShip.ship', 'admin.order.pendingShip.shipTitle', " +
			"'admin.order.shipForm.order', 'admin.order.shipForm.total', 'admin.order.shipForm.col.product', " +
			"'admin.order.shipForm.col.quantity', 'admin.order.shipForm.col.shipped', 'admin.order.shipForm.col.remaining', " +
			"'admin.order.shipForm.col.thisTime', 'admin.order.shipForm.col.warehouse', 'admin.order.shipForm.warehouseMissing', " +
			"'admin.order.shipForm.carrier', 'admin.order.shipForm.carrierNone', 'admin.order.shipForm.trackingNo', " +
			"'admin.order.shipForm.trackingNoPh', 'admin.order.shipForm.remark', 'admin.order.shipForm.submit', " +
			"'admin.order.carriers.title', 'admin.order.carriers.hint', 'admin.order.carriers.sub', " +
			"'admin.order.carriers.create', 'admin.order.carriers.listLead', 'admin.order.carriers.col.name', " +
			"'admin.order.carriers.col.code', 'admin.order.carriers.col.status', 'admin.order.carriers.col.trackingUrl', " +
			"'admin.order.carriers.col.sort', 'admin.order.carriers.emptyHeading', 'admin.order.carriers.emptyDesc', " +
			"'admin.order.carriers.active', 'admin.order.carriers.disabled', 'admin.order.carriers.enable', " +
			"'admin.order.carriers.disable', 'admin.order.carriers.codePh', 'admin.order.carriers.namePh', " +
			"'admin.order.carriers.trackingUrlPh', 'admin.order.carriers.trackingUrlHint', 'order.err.orderNotShippable', " +
			"'order.err.shipQuantityExceeded', 'order.err.shipLinesRequired', 'order.err.shipRequestKeyConflict', " +
			"'order.err.shipWarehouseRequired', 'order.err.carrierNotFound', 'order.err.carrierCodeTaken', " +
			"'order.err.carrierTemplateInvalid', 'order.err.carrierStatusInvalid')",
		SQL: mustSQL("629_order_shipment_pages_i18n.sql"),
	})

	registerSeed(Seed{
		Version:   "632-sales-overview-w4-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 36 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.order.sales.label.units', 'admin.order.sales.note.units', 'admin.order.sales.costIncomplete', " +
			"'admin.order.sales.costIncompleteTail', 'admin.order.sales.todo.titleLead', 'admin.order.sales.todo.titleTail', " +
			"'admin.order.sales.label.grossProfit', 'admin.order.sales.note.grossProfit', 'inventory.stock.alert.title', " +
			"'inventory.stock.alert.total', 'inventory.stock.alert.outOfStock', 'inventory.stock.alert.lowStock', " +
			"'inventory.stock.alert.alert', 'inventory.stock.alert.hint', 'admin.order.sales.todo.transfers', " +
			"'admin.order.sales.todo.alerts', 'admin.order.sales.todo.pendingShip', 'admin.order.sales.todo.empty')",
		SQL: mustSQL("632_sales_overview_w4_i18n.sql"),
	})
}
