package migrations

func registerCatalogAndInventory() {

	registerSeed(Seed{
		Version:      "087-product-variant-generate-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'product:variant_generate'",
		SQL:          mustSQL("087_product_variant_generate_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "087b-product-variant-options-template",
		TableName:    "content_templates",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM content_templates WHERE entity_type = 'product' AND draft_document::text NOT LIKE '%\"optionsField\"%'",
		SQL:          mustSQL("087b_product_variant_options_template.sql"),
	})

	register(Migration{
		Version:   "088-product-taxonomy",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'primary_category_id'",
		SQL: mustSQL("088_product_taxonomy.sql"),
	})

	registerSeed(Seed{
		Version:   "089-product-taxonomy-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:category_list', 'product:category_get', 'product:category_create', " +
			"'product:category_update', 'product:category_delete', " +
			"'product:brand_list', 'product:brand_get', 'product:brand_create', " +
			"'product:brand_update', 'product:brand_delete')",
		SQL: mustSQL("089_product_taxonomy_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "090-product-taxonomy-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_menus WHERE title IN ('商品分类', '商品品牌') AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("090_product_taxonomy_menu.sql"),
	})

	register(Migration{
		Version:   "091-product-tags",
		TableName: "products",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name IN (?, 'product_tags') " +
			"AND column_name IN ('published_at', 'recalc_at')",
		SQL: mustSQL("091_product_tags.sql"),
	})

	registerSeed(Seed{
		Version:   "092-product-tag-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:tag_list', 'product:tag_get', 'product:tag_products', 'product:tag_rule_types', " +
			"'product:tag_create', 'product:tag_update', 'product:tag_delete', 'product:tag_recalc')",
		SQL: mustSQL("092_product_tag_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "093-product-tag-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '商品标签' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("093_product_tag_menu.sql"),
	})

	register(Migration{
		Version:   "095-product-pricing",
		TableName: "product_price_adjustments",
		SQL:       mustSQL("095_product_pricing.sql"),
	})

	registerSeed(Seed{
		Version:   "096-product-pricing-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:pricing_rules', 'product:pricing_roundings', 'product:pricing_preview', " +
			"'product:pricing_apply', 'product:pricing_history', 'product:pricing_adjustment')",
		SQL: mustSQL("096_product_pricing_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "097-product-pricing-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '定价工具' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("097_product_pricing_menu.sql"),
	})

	registerSeed(Seed{
		Version:   "098-product-detail-template-choose-perms",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'presentation:preview', 'presentation:get_by_entity')",
		SQL: mustSQL("098_product_detail_template_choice_permissions.sql"),
	})

	register(Migration{
		Version:   "094-product-image-alts",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'images_alt'",
		SQL: mustSQL("094_product_image_alts.sql"),
	})

	register(Migration{
		Version:   "086-product-attribute-specs",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'attribute_ids'",
		SQL: mustSQL("086_product_attribute_specs.sql"),
	})

	registerSeed(Seed{
		Version:      "086a-product-attribute-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'product:attribute_%'",
		SQL:          mustSQL("086a_product_attribute_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "086b-product-attribute-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE title = '商品属性' AND type = 2",
		SQL:          mustSQL("086b_product_attribute_menu.sql"),
	})

	registerSeed(Seed{
		Version:      "085-product-detail-template",
		TableName:    "content_templates",
		ConditionSQL: "SELECT COUNT(*) FROM content_templates WHERE entity_type = 'product'",
		SQL:          mustSQL("085_product_detail_template.sql"),
	})

	registerSeed(Seed{
		Version:      "084-product-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE title = '商品管理' AND type = 2",
		SQL:          mustSQL("084_product_menu.sql"),
	})

	register(Migration{
		Version:   "083-product-variant-options-unique",
		TableName: "product_variants",
		CheckSQL: `SELECT COUNT(*) FROM pg_indexes i
			WHERE i.schemaname = current_schema() AND i.tablename = ?
			  AND i.indexname = 'uq_product_variants_product_options'
			  AND NOT EXISTS (
			      SELECT 1 FROM pg_constraint c
			      WHERE c.conrelid = 'product_variants'::regclass
			        AND c.conname = 'product_variants_product_id_option_values_key'
			  )`,
		SQL: mustSQL("083_product_variant_options_unique.sql"),
	})

	registerSeed(Seed{
		Version:      "082-product-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'product:%'",
		SQL:          mustSQL("082_product_permissions.sql"),
	})

	register(Migration{
		Version:   "081-product-tables",
		TableName: "products",
		SQL:       mustSQL("081_product_tables.sql"),
	})

	register(Migration{
		Version:   "080-content-type-narrowing",
		TableName: "contents",
		CheckSQL: `SELECT COUNT(*) FROM pg_constraint
			WHERE conrelid = ?::regclass
			  AND conname = 'contents_entity_type_check'
			  AND pg_get_constraintdef(oid) LIKE '%article%'
			  AND pg_get_constraintdef(oid) NOT LIKE '%product%'`,
		SQL: mustSQL("080_content_type_narrowing.sql"),
	})

	register(Migration{
		Version:   "099-inventory-tables",
		TableName: "inventory_warehouses",
		SQL:       mustSQL("099_inventory_tables.sql"),
	})

	registerSeed(Seed{
		Version:   "100-inventory-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 9 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:warehouse_list', 'inventory:warehouse_get', 'inventory:warehouse_create', " +
			"'inventory:warehouse_update', 'inventory:warehouse_delete', " +
			"'inventory:stock_list', 'inventory:stock_sku', 'inventory:stock_get', 'inventory:stock_ensure')",
		SQL: mustSQL("100_inventory_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "101-inventory-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '库存管理' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("101_inventory_menu.sql"),
	})

	register(Migration{
		Version:   "102-inventory-movements",
		TableName: "inventory_change_reasons",
		SQL:       mustSQL("102_inventory_movements.sql"),
	})

	registerSeed(Seed{
		Version:   "103-inventory-reasons-seed",
		TableName: "inventory_change_reasons",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 9 THEN 1 ELSE 0 END FROM inventory_change_reasons " +
			"WHERE project_id IS NULL AND lower(code) IN (" +
			"'purchase_in', 'return_in', 'transfer_in', 'production_in', " +
			"'sale_out', 'damage_out', 'transfer_out', 'stocktake_adjust', 'manual_adjust')",
		SQL: mustSQL("103_inventory_reasons_seed.sql"),
	})

	registerSeed(Seed{
		Version:   "104-inventory-change-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:stock_change', 'inventory:stock_deduct', 'inventory:movement_list', " +
			"'inventory:reason_list', 'inventory:reason_create', 'inventory:reason_update')",
		SQL: mustSQL("104_inventory_change_permissions.sql"),
	})

	register(Migration{
		Version:   "105-inventory-sources",
		TableName: "inventory_sources",
		SQL:       mustSQL("105_inventory_sources.sql"),
	})

	registerSeed(Seed{
		Version:   "106-inventory-source-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:source_list', 'inventory:source_get', 'inventory:source_create', " +
			"'inventory:source_update', 'inventory:source_delete', 'inventory:source_summary')",
		SQL: mustSQL("106_inventory_source_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "107-inventory-source-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '货源管理' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("107_inventory_source_menu.sql"),
	})

	register(Migration{
		Version:   "108-inventory-purchase",
		TableName: "inventory_purchase_orders",
		SQL:       mustSQL("108_inventory_purchase.sql"),
	})

	registerSeed(Seed{
		Version:   "109-inventory-purchase-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 7 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:purchase_list', 'inventory:purchase_get', 'inventory:purchase_create', " +
			"'inventory:purchase_update', 'inventory:purchase_receipt', " +
			"'inventory:purchase_production', 'inventory:purchase_history')",
		SQL: mustSQL("109_inventory_purchase_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "110-inventory-purchase-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '采购入库' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("110_inventory_purchase_menu.sql"),
	})

	register(Migration{
		Version:   "111-master-data-changes",
		TableName: "master_data_changes",
		// 判定与 SQL 保持历史原文：两个时间索引由 763-master-data-changes-time-indexes
		// 补建（原文用 created_at，存量/终态库重跑会撞已改名的 create_time）。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM (" +
			"SELECT 1 FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"UNION ALL " +
			"SELECT 1 FROM pg_trigger t " +
			"JOIN pg_class c ON c.oid = t.tgrelid " +
			"JOIN pg_namespace n ON n.oid = c.relnamespace " +
			"WHERE n.nspname = current_schema() AND c.relname = 'master_data_changes' " +
			"AND t.tgname = 'trg_master_data_changes_append_only' AND NOT t.tgisinternal" +
			") x",
		SQL: mustSQL("111_master_data_changes.sql"),
	})

	registerSeed(Seed{
		Version:   "112-master-data-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'masterdata:change_list', 'masterdata:change_count', " +
			"'masterdata:change_entities', 'masterdata:change_entity')",
		SQL: mustSQL("112_master_data_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "113-master-data-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '变更记录' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("113_master_data_menu.sql"),
	})

	register(Migration{
		Version:   "117-product-brand-index",
		TableName: "products",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_products_brand_id'",
		SQL: mustSQL("117_product_brand_index.sql"),
	})

	register(Migration{
		Version:   "118-product-variant-option-index",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_product_variants_option_values'",
		SQL: mustSQL("118_product_variant_option_index.sql"),
	})

	register(Migration{
		Version:   "119-product-variant-price-index",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_product_variants_price_enabled'",
		SQL: mustSQL("119_product_variant_price_index.sql"),
	})

	register(Migration{
		Version:   "120-product-rating",
		TableName: "product_ratings",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?",
		SQL: mustSQL("120_product_rating.sql"),
	})

	register(Migration{
		Version:   "121-drop-stock-cache",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"AND column_name IN ('stock_total', 'stock_synced_at')",
		SQL: mustSQL("121_drop_stock_cache.sql"),
	})

	register(Migration{
		Version:   "122-drop-cache-permissions",
		TableName: "inventory:cache_sync",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_permission " +
			"WHERE permission_code IN (?, 'inventory:cache_reconcile')",
		SQL: mustSQL("122_drop_cache_permissions.sql"),
	})

	register(Migration{
		Version:   "073-blueprint-ddl-align",
		TableName: "blueprints",
		CheckSQL: `SELECT COUNT(*) FROM information_schema.tables t
			WHERE t.table_schema = current_schema() AND t.table_name = ?
			  AND NOT EXISTS (
			      SELECT 1 FROM information_schema.columns c
			      WHERE c.table_schema = t.table_schema AND c.table_name = t.table_name
			        AND c.column_name IN ('project_id','source_hash','created_by')
			  )`,
		SQL: mustSQL("073_blueprint_ddl_align.sql"),
	})

	register(Migration{
		Version:   "229-inventory-menu-split",
		TableName: "sys_menus",
		CheckSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND type = 1 " +
			"AND COALESCE(parent_id, 0) = 0 AND title = '库存') >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND type = 2 " +
			"AND path = '/admin/inventory/warehouses') >= 1 " +
			"AND (SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND type = 2 " +
			"AND path = '/admin/inventory/reasons') >= 1 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("229_inventory_menu_split.sql"),
	})

	register(Migration{
		Version:   "236-fix-inventory-menu-visibility",
		TableName: "sys_menus",

		CheckSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 0 ELSE 1 END FROM sys_menus " +
			"WHERE to_regclass(?) IS NOT NULL AND deleted_at IS NULL AND is_public = 1 AND (" +
			"path IN ('/admin/inventory/warehouses','/admin/inventory/reasons') " +
			"OR (type = 1 AND parent_id = 0 AND title = '库存'))",
		SQL: mustSQL("236_fix_inventory_menu_visibility.sql"),
	})

	register(Migration{
		Version:   "240-inventory-warehouse-type-config",
		TableName: "inventory_warehouses",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM (" +
			"SELECT 1 FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name IN ('type', 'config') " +
			"UNION ALL " +
			"SELECT 1 FROM pg_constraint " +
			"WHERE conrelid = 'inventory_warehouses'::regclass " +
			"AND conname = 'inventory_warehouses_type_check' AND convalidated" +
			") x",
		SQL: mustSQL("240_inventory_warehouse_type_config.sql"),
	})

	registerSeed(Seed{
		Version:   "241-inventory-reason-i18n-key",
		TableName: "inventory_change_reasons",

		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM inventory_change_reasons " +
			"WHERE name <> '' AND name NOT LIKE 'inventory.reason.%'",
		SQL: mustSQL("241_inventory_reason_i18n_key.sql"),
	})

	registerSeed(Seed{
		Version:      "242-inventory-i18n-seed",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'inventory.reason.purchase_in'",
		SQL:          mustSQL("242_inventory_reason_i18n.sql"),
	})

	registerSeed(Seed{
		Version:      "243-inventory-page-i18n-seed",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.inventory.adjust.title'",
		SQL:          mustSQL("243_i18n_seed_inventory_pages.sql"),
	})

	register(Migration{
		Version:   "244-inventory-stock-cost",
		TableName: "inventory_stocks",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM (" +
			"SELECT 1 FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'cost_price' " +
			"UNION ALL " +
			"SELECT 1 FROM pg_constraint " +
			"WHERE conrelid = 'inventory_stocks'::regclass " +
			"AND conname = 'uq_inventory_stocks_warehouse_sku' " +
			"AND contype = 'u' AND convalidated" +
			") x",
		SQL: mustSQL("244_inventory_stock_cost.sql"),
	})

	registerSeed(Seed{
		Version:      "245-i18n-seed-inventory-cost",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrStockCostInvalid'",
		SQL:          mustSQL("245_i18n_seed_inventory_cost.sql"),
	})

	register(Migration{
		Version:   "246-product-container-sku",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'sku_code'",
		SQL: mustSQL("246_product_container_sku.sql"),
	})

	registerSeed(Seed{
		Version:   "247-i18n-seed-product-followups",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('admin.product_translations.noTargetLang.desc'),
    ('admin.product_translations.noTargetLang.descTail'),
    ('admin.products.bulkPricing.previewHintLead'),
    ('admin.products.bulkPricing.previewHintTail'),
    ('admin.products.create.noAttributesLead'),
    ('admin.products.create.noAttributesTail'),
    ('pBulkScopeHint'),
    ('pNewAttrs'),
    ('trNoLangTitle'),
    ('trNoTgtLangAct')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723e_current_247_i18n_seed_product_followups.sql"),
	})

	registerSeed(Seed{
		Version:   "248-i18n-seed-product-sku",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('pBoxSkuInvalid'),
    ('pSkuBoxMissing'),
    ('pSkuLabel'),
    ('pSkuPh'),
    ('pdSkuScope')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723f_current_248_product_sku_error_i18n.sql"),
	})

	registerSeed(Seed{
		Version:   "250-admin-menu-product-scope",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_menus m " +
			"WHERE m.deleted_at IS NULL AND (" +
			"(m.status <> 0 AND m.path IN ('/admin/product-pricing','/admin/products/bundle')) " +
			"OR (m.path = '/admin/products/template' " +
			"AND EXISTS (SELECT 1 FROM sys_menus p WHERE coalesce(p.parent_id, 0) = 0 AND p.type = 1 AND p.title = '内容' AND p.deleted_at IS NULL) " +
			"AND m.parent_id IS DISTINCT FROM (SELECT p2.id FROM sys_menus p2 WHERE coalesce(p2.parent_id, 0) = 0 AND p2.type = 1 AND p2.title = '内容' AND p2.deleted_at IS NULL ORDER BY p2.id ASC LIMIT 1)))",
		SQL: mustSQL("250_admin_menu_product_scope.sql"),
	})

	register(Migration{
		Version:   "251-inventory-external-sku",
		TableName: "inventory_stocks",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM (" +
			"SELECT 1 FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'external_sku' " +
			"UNION ALL " +
			"SELECT 1 FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = 'inventory_stocks' " +
			"AND indexname = 'idx_inventory_stocks_warehouse_external_sku'" +
			") x",
		SQL: mustSQL("251_inventory_external_sku.sql"),
	})

	registerSeed(Seed{
		Version:   "252-i18n-seed-inventory-external-sku",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM (VALUES
    ('ErrExternalSKUInvalid'),
    ('ErrExternalSKUProductConflict'),
    ('ErrSKUSourceInvalid'),
    ('ErrWarehouseSKUCodeTaken'),
    ('ErrWarehouseSKUNotFound'),
    ('ErrWarehouseSKURequired'),
    ('admin.inventory.sku.col.externalSku'),
    ('admin.products.label.warehouseSku'),
    ('admin.products.ph.warehouseSku'),
    ('admin.products.skuSource.custom'),
    ('admin.products.skuSource.label'),
    ('admin.products.skuSource.previewLabel'),
    ('admin.products.skuSource.warehouse'),
    ('pExternalSku'),
    ('pExternalSkuPh'),
    ('pSkuSrcWhHint')) AS keys(item_key) CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)) THEN 1 ELSE 0 END`,
		SQL: mustSQL("723g_current_252_i18n_seed_inventory_external_sku.sql"),
	})

	registerSeed(Seed{
		Version:      "255-i18n-seed-inventory-external-sku-edit",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.inventory.sku.externalSku.save'",
		SQL:          mustSQL("255_inventory_external_sku_edit_i18n.sql"),
	})

	register(Migration{
		Version:   "261-inventory-stock-track-quantity",
		TableName: "inventory_stocks",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'track_quantity'",
		SQL: mustSQL("261_inventory_stock_track_quantity.sql"),
	})

	register(Migration{
		Version:   "262-inventory-stock-sku-strip-warehouse-prefix",
		TableName: "inventory_stocks",
		CheckSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 0 ELSE 1 END FROM inventory_stocks s " +
			"JOIN inventory_warehouses w ON w.id = s.warehouse_id " +
			"WHERE to_regclass(?) IS NOT NULL " +
			"AND length(s.sku_code) > length(w.code) + 1 " +
			"AND upper(left(s.sku_code, length(w.code))) = upper(w.code) " +
			"AND substr(s.sku_code, length(w.code) + 1, 1) = '_'",
		SQL: mustSQL("262_inventory_stock_sku_strip_warehouse_prefix.sql"),
	})

	registerSeed(Seed{
		Version:      "263-i18n-seed-inventory-track-quantity",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.inventory.stock.unlimited'",
		SQL:          mustSQL("263_inventory_track_quantity_i18n.sql"),
	})

	register(Migration{
		Version:   "624-inventory-stock-master",
		TableName: "inventory_stocks",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'reserved_quantity'",
		SQL: mustSQL("624_inventory_stock_master.sql"),
	})

	registerSeed(Seed{
		Version:   "625-inventory-stock-master-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'ErrReserveUnderflow', 'admin.inventory.filter.all')",
		SQL: mustSQL("625_inventory_stock_master_i18n.sql"),
	})

	register(Migration{
		Version:   "626-inventory-deduct-priority",
		TableName: "inventory_warehouses",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'deduct_priority'",
		SQL: mustSQL("626_inventory_deduct_priority.sql"),
	})

	registerSeed(Seed{
		Version:   "626b-inventory-deduct-priority-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 20 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.inventory.warehouse.col.deductPriority', 'admin.inventory.warehouse.deductPriorityHint', " +
			"'admin.inventory_purchases.status.finished', 'admin.inventory_purchases.status.cancelled', " +
			"'admin.inventory_purchases.finish', 'admin.inventory_purchases.finishConfirm', " +
			"'admin.inventory_purchases.cancel', 'admin.inventory_purchases.cancelConfirm', " +
			"'ErrPurchaseStatusNotFinishable', 'ErrPurchaseStatusNotCancelable')",
		SQL: mustSQL("626b_inventory_deduct_priority_i18n.sql"),
	})

	register(Migration{
		Version:   "630-inventory-transfer",
		TableName: "inventory_transfers",
		CheckSQL: allChecks(
			"SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM information_schema.tables "+
				"WHERE table_schema = current_schema() AND (CAST(? AS text) IS NOT NULL) "+
				"AND table_name IN ('inventory_transfers', 'inventory_transfer_items', 'inventory_transfer_attachments')",
			coverObjects("inventory_transfers", "uq_inventory_transfers_no", "uq_inventory_transfers_request", "idx_inventory_transfers_status", "inventory_transfer_items", "uq_inventory_transfer_items_row", "inventory_transfer_attachments", "uq_inventory_transfer_attachments_row"),
		),
		SQL: mustSQL("630_inventory_transfer.sql"),
	})

	registerSeed(Seed{
		Version:   "631-inventory-transfer-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 90 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.inventory.transfers.title', 'admin.inventory.transfers.hint', 'admin.inventory.transfers.sub', " +
			"'admin.inventory.transfers.create', 'admin.inventory.transfers.listLead', 'admin.inventory.transfers.filter.keywordPh', " +
			"'admin.inventory.transfers.col.no', 'admin.inventory.transfers.col.route', 'admin.inventory.transfers.col.status', " +
			"'admin.inventory.transfers.col.items', 'admin.inventory.transfers.col.totalQty', 'admin.inventory.transfers.col.createdAt', " +
			"'admin.inventory.transfers.emptyHeading', 'admin.inventory.transfers.emptyDesc', 'admin.inventory.transfers.ship', " +
			"'admin.inventory.transfers.receive', 'admin.inventory.transfers.cancel', 'admin.inventory.transfers.cancelConfirm', " +
			"'admin.inventory.transfers.cancelRemark', 'admin.inventory.transfers.attachments', 'admin.inventory.transfers.shipAttachHint', " +
			"'admin.inventory.transfers.receiveAttachHint', 'admin.inventory.transfers.remark', 'admin.inventory.transfers.shipSubmit', " +
			"'admin.inventory.transfers.receiveSubmit', 'admin.inventory.transfers.fromWarehouse', 'admin.inventory.transfers.toWarehouse', " +
			"'admin.inventory.transfers.sameWarehouseHint', 'admin.inventory.transfers.requestAttachments', 'admin.inventory.transfers.createSubmit', " +
			"'admin.inventory.transfers.status.pending', 'admin.inventory.transfers.status.in_transit', 'admin.inventory.transfers.status.completed', " +
			"'admin.inventory.transfers.status.cancelled', 'ErrTransferNotFound', 'ErrTransferLinesRequired', " +
			"'ErrTransferSameWarehouse', 'ErrItemNotInSourceWarehouse', 'ErrTransferStatusInvalid', " +
			"'ErrAttachmentRequired', 'ErrAttachmentNotFound', 'ErrStockNotEnough', 'admin.inventory.transfers.units', 'admin.inventory.boundary.transfers', 'admin.inventory.boundary.tail2')",
		SQL: mustSQL("631_inventory_transfer_i18n.sql"),
	})
}
