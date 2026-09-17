package migrations

// registerCatalogAndInventory 注册「商品与库存域（073、080–122）」。
//
// 由 register.go 的 init() 按主题拆出：本文件只做注册，不含任何 SQL 文本，
// 注册项引用的 SQL 一律经 mustSQL 从嵌入的 .sql 取。
func registerCatalogAndInventory() {
	// 087：变体组合生成权限点 + 超管策略（issue #8）。条件只看本票自己的权限点，
	// 与 082/086a 的宽匹配（product:% / product:attribute_%）互不干扰。
	registerSeed(Seed{
		Version:      "087-product-variant-generate-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'product:variant_generate'",
		SQL:          mustSQL("087_product_variant_generate_permissions.sql"),
	})

	// 087b：默认商品详情模板补规格槽位（issue #8）。085 只在首次建库执行，
	// 已执行过的库不会重跑，故这里补一次；DO 块内只重写与 085 原样一致的模板，
	// 作者改过的模板不动（缺槽位是作者的选择）。
	//
	// ConditionSQL 的语义是「已存在则跳过」（与其他种子一致）：所有 product 模板
	// 都已带 optionsField 时返回 1 跳过；仍有缺槽位的模板时返回 0 → 跑 DO 块。
	registerSeed(Seed{
		Version:      "087b-product-variant-options-template",
		TableName:    "content_templates",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM content_templates WHERE entity_type = 'product' AND draft_document::text NOT LIKE '%\"optionsField\"%'",
		SQL:          mustSQL("087b_product_variant_options_template.sql"),
	})

	// 088：商品 → 主分类列（issue #10）。product_categories / product_brands 两张表
	// 由 081 建好，本迁移只补 products.primary_category_id 与其索引。
	// products 早已存在，默认「表存在即跳过」必然误跳过，故按列是否存在判定
	// （与 067/086 同一手法）。
	register(Migration{
		Version:   "088-product-taxonomy",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'primary_category_id'",
		SQL: mustSQL("088_product_taxonomy.sql"),
	})

	// 089：分类与品牌权限点 + 超管策略（issue #10）。
	// 条件只看本票自己的权限点，与 082 的宽匹配（product:%）互不干扰。
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

	// 090：分类与品牌后台菜单（issue #10）。须在 084（商品管理菜单）之后执行，
	// 否则父菜单还不存在，COALESCE 会把两个入口落到顶级。
	registerSeed(Seed{
		Version:      "090-product-taxonomy-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_menus WHERE title IN ('商品分类', '商品品牌') AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("090_product_taxonomy_menu.sql"),
	})

	// 091：商品标签规则化（issue #11）—— product_tags 由 081 建好、products 早已存在，
	// 本迁移补 published_at（新品规则的时间基准）/ recalc_at（重算时间）+ 规则形状约束。
	// 两张表都早已存在，默认「表存在即跳过」必然误跳过，故按列是否存在判定：
	// 恰好一个 ? 参数（products），另一张表用字面量表名（与 070 同一手法）。
	register(Migration{
		Version:   "091-product-tags",
		TableName: "products",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name IN (?, 'product_tags') " +
			"AND column_name IN ('published_at', 'recalc_at')",
		SQL: mustSQL("091_product_tags.sql"),
	})

	// 092：标签权限点 + 超管策略（issue #11）。
	// 条件只看本票自己的权限点，与 082 的宽匹配（product:%）互不干扰。
	registerSeed(Seed{
		Version:   "092-product-tag-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:tag_list', 'product:tag_get', 'product:tag_products', 'product:tag_rule_types', " +
			"'product:tag_create', 'product:tag_update', 'product:tag_delete', 'product:tag_recalc')",
		SQL: mustSQL("092_product_tag_permissions.sql"),
	})

	// 093：标签后台菜单（issue #11）。须在 084（商品管理菜单）之后执行，
	// 否则父菜单还不存在，COALESCE 会把入口落到顶级。
	registerSeed(Seed{
		Version:      "093-product-tag-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '商品标签' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("093_product_tag_menu.sql"),
	})

	// 095：商品定价工具留痕（issue #13）—— 两张全新表（调价批次 + 逐变体明细）。
	// 表此前不存在，默认「表存在即跳过」即可；定价结果写回 product_variants.price，
	// 不新增任何构建期读取路径（不进构建管线）。
	register(Migration{
		Version:   "095-product-pricing",
		TableName: "product_price_adjustments",
		SQL:       mustSQL("095_product_pricing.sql"),
	})

	// 096：定价工具权限点 + 超管策略（issue #13）。
	// 条件只看本票自己的权限点，与 082 的宽匹配（product:%）互不干扰。
	registerSeed(Seed{
		Version:   "096-product-pricing-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:pricing_rules', 'product:pricing_roundings', 'product:pricing_preview', " +
			"'product:pricing_apply', 'product:pricing_history', 'product:pricing_adjustment')",
		SQL: mustSQL("096_product_pricing_permissions.sql"),
	})

	// 097：定价工具后台菜单（issue #13）。须在 084（商品管理菜单）之后执行，
	// 否则父菜单还不存在，COALESCE 会把入口落到顶级。
	registerSeed(Seed{
		Version:      "097-product-pricing-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '定价工具' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("097_product_pricing_menu.sql"),
	})

	// 098：详情页模板可选与预览权限点 + 超管策略（issue #14）。
	// 两个新接口（preview 只读渲染 / get-by-entity 读当前绑定）各自一个权限点；
	// 后台「详情页模板」页的写动作复用既有 presentation:create / presentation:rebuild。
	registerSeed(Seed{
		Version:   "098-product-detail-template-choose-perms",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'presentation:preview', 'presentation:get_by_entity')",
		SQL: mustSQL("098_product_detail_template_choice_permissions.sql"),
	})

	// 094：商品图集 alt 文本列（issue #12 商品多语言）。
	// products 由 081 创建，默认「表存在即跳过」必然误跳过，故按列是否存在判定
	// （与 067/091 同一手法）：images_alt 列存在即视为已完成。
	register(Migration{
		Version:   "094-product-image-alts",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'images_alt'",
		SQL: mustSQL("094_product_image_alts.sql"),
	})

	// 086：商品属性组与属性值（issue #7）—— product_attributes 由 081 建好，
	// 本迁移在 **products** 上补 attribute_ids 列，并在 product_attributes 上补 key 唯一的部分
	// 索引、回填历史空 key、加 is_variation 的显式 CHECK。
	//
	// 判定对象必须与 SQL 实际改的表一致（2026-09-17 修）：
	//   · 不能按表名判定 —— product_attributes 在 081 就建好了，会误跳过整条迁移；
	//   · 更不能用它查 product_attributes 的列 —— attribute_ids 是 **products** 的列，
	//     拿着张冠李戴的对象去判 ⇒ 计数恒为 0 ⇒ 这条迁移**每次启动都重跑**。
	//     超级用户下靠 SQL 里的 IF NOT EXISTS 兜住了，所以一直没人发现（AGENTS.md 记的
	//     178 是同一种「判定恒 0」形态）；一旦换成非超级角色连接（DB-009 换角色），
	//     重跑会撞 "must be owner of table products (42501)"，**应用直接启动失败**。
	//     实测：2026-09-17 换角色预演就是这么炸的（ALTER TABLE 的所有权检查与 IF NOT EXISTS 无关）。
	register(Migration{
		Version:   "086-product-attribute-specs",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'attribute_ids'",
		SQL: mustSQL("086_product_attribute_specs.sql"),
	})

	// 086a：属性组权限点 + 超管策略（issue #7）。条件与 082 互斥（product:attribute_%）。
	registerSeed(Seed{
		Version:      "086a-product-attribute-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'product:attribute_%'",
		SQL:          mustSQL("086a_product_attribute_permissions.sql"),
	})

	// 086b：属性后台菜单（issue #7）。须在 084（商品管理菜单）之后执行，
	// 否则父菜单还不存在，COALESCE 会把「商品属性」落到顶级。
	registerSeed(Seed{
		Version:      "086b-product-attribute-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE title = '商品属性' AND type = 2",
		SQL:          mustSQL("086b_product_attribute_menu.sql"),
	})

	// 085：默认商品详情内容模板（issue #6）—— 商品发布按实体类型解析模板，
	// 种一份类型级默认模板即可让「新建商品即用上」，无需逐商品手工拼装。
	// 数据种子：内容模板行 + 首个不可变版本 + current_version_id 指针，
	// 由 DO 块一次写入（applySeed 以单条语句执行整段 SQL）。
	registerSeed(Seed{
		Version:      "085-product-detail-template",
		TableName:    "content_templates",
		ConditionSQL: "SELECT COUNT(*) FROM content_templates WHERE entity_type = 'product'",
		SQL:          mustSQL("085_product_detail_template.sql"),
	})

	// 084：商品管理后台菜单（issue #5）。
	registerSeed(Seed{
		Version:      "084-product-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT COUNT(*) FROM sys_menus WHERE title = '商品管理' AND type = 2",
		SQL:          mustSQL("084_product_menu.sql"),
	})

	// 083：修正 product_variants 的规格组合唯一约束（改部分唯一索引）。
	// 按「索引已存在且旧约束已消失」判定跳过。
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

	// 082：商品域权限点 + 超管策略（issue #5）。
	registerSeed(Seed{
		Version:      "082-product-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code LIKE 'product:%'",
		SQL:          mustSQL("082_product_permissions.sql"),
	})

	// 081：商品域六张表（issue #5）。products 是新建表，默认「表存在即跳过」即可。
	register(Migration{
		Version:   "081-product-tables",
		TableName: "products",
		SQL:       mustSQL("081_product_tables.sql"),
	})

	// 080：内容类型收敛 —— contents 只保留 article；content_templates 解掉类型枚举
	// （合法性交给实体类型注册表）；pages 内容契约去掉 product / category 分支。
	// 三张表都由更早的迁移建立，本迁移只改约束，故按「约束已收敛」判定跳过。
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

	// 099：仓库与库存记录（issue #15）。两张新表（inventory_warehouses / inventory_stocks），
	// 默认「表存在即跳过」检查即可 —— 库存真源与仓库实体都是本票新建的对象。
	register(Migration{
		Version:   "099-inventory-tables",
		TableName: "inventory_warehouses",
		SQL:       mustSQL("099_inventory_tables.sql"),
	})

	// 100：仓库与库存权限点 + 超管策略（issue #15）。
	// 条件只看本票自己的权限点（inventory:%），与其它模块的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "100-inventory-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 9 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:warehouse_list', 'inventory:warehouse_get', 'inventory:warehouse_create', " +
			"'inventory:warehouse_update', 'inventory:warehouse_delete', " +
			"'inventory:stock_list', 'inventory:stock_sku', 'inventory:stock_get', 'inventory:stock_ensure')",
		SQL: mustSQL("100_inventory_permissions.sql"),
	})

	// 101：库存管理后台菜单（issue #15）。须在 084（商品管理菜单）之后执行，
	// 否则「站点工程」目录还不存在时 COALESCE 会把入口落到顶级。
	registerSeed(Seed{
		Version:      "101-inventory-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '库存管理' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("101_inventory_menu.sql"),
	})

	// 102：库存流水 / 变动原因字典 / 物料清单 / 缓存同步台账（issue #16）。
	// 四张全新表，默认「表存在即跳过」检查即可；库存**真源** inventory_stocks
	// 的结构一个字不动（行锁加在既有表既有的行上）。
	register(Migration{
		Version:   "102-inventory-movements",
		TableName: "inventory_change_reasons",
		SQL:       mustSQL("102_inventory_movements.sql"),
	})

	// 103：内置变动原因字典（in / out / adjust 三类各若干条，issue #16）。
	// 单条 INSERT：project_id IS NULL 表示内置（全工程可见）；
	// 幂等条件 = 每个内置 code 都已存在，新增内置原因后重启即补齐。
	registerSeed(Seed{
		Version:   "103-inventory-reasons-seed",
		TableName: "inventory_change_reasons",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 9 THEN 1 ELSE 0 END FROM inventory_change_reasons " +
			"WHERE project_id IS NULL AND lower(code) IN (" +
			"'purchase_in', 'return_in', 'transfer_in', 'production_in', " +
			"'sale_out', 'damage_out', 'transfer_out', 'stocktake_adjust', 'manual_adjust')",
		SQL: mustSQL("103_inventory_reasons_seed.sql"),
	})

	// 104：库存变动 / 流水 / 原因字典 / 物料清单 / 缓存对账 10 个权限点 + 超管策略（issue #16）。
	// 条件只看本票自己的权限点，与 100 的宽匹配（inventory:%）互不干扰。
	registerSeed(Seed{
		Version:   "104-inventory-change-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 10 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:stock_change', 'inventory:stock_deduct', 'inventory:movement_list', " +
			"'inventory:reason_list', 'inventory:reason_create', 'inventory:reason_update', " +
			"'inventory:bom_set', 'inventory:bom_get', 'inventory:cache_sync', 'inventory:cache_reconcile')",
		SQL: mustSQL("104_inventory_change_permissions.sql"),
	})

	// 105：货源表（issue #17）。一张新表承载全部进货来源（外部供应商 / 集团内关联公司 /
	// 自家工厂），类型 + 关联方标志 + 异构对接配置（config）三件事各就各位。
	register(Migration{
		Version:   "105-inventory-sources",
		TableName: "inventory_sources",
		SQL:       mustSQL("105_inventory_sources.sql"),
	})

	// 106：货源管理 6 个权限点 + 超管策略（issue #17）。
	// 条件只看本票自己的权限点（inventory:source_%），与 100 的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "106-inventory-source-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:source_list', 'inventory:source_get', 'inventory:source_create', " +
			"'inventory:source_update', 'inventory:source_delete', 'inventory:source_summary')",
		SQL: mustSQL("106_inventory_source_permissions.sql"),
	})

	// 107：货源管理后台菜单（issue #17）。须在 101（库存管理菜单）之后执行，
	// 且与它同挂「站点工程」目录（sort 9，排在库存管理 sort 8 之后）。
	registerSeed(Seed{
		Version:      "107-inventory-source-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '货源管理' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("107_inventory_source_menu.sql"),
	})

	// 108：采购单与入库四张表（issue #18）：采购单头 / 采购行（含已入库数量）/
	// 入库单头（采购收货与自家工厂生产入库共用，带幂等键）/ 入库单行。
	// 四张全新表，默认「表存在即跳过」检查即可；库存**真源** inventory_stocks
	// 一张不加、一列不改 —— 入库一律经 #16 的变动契约写它。
	register(Migration{
		Version:   "108-inventory-purchase",
		TableName: "inventory_purchase_orders",
		SQL:       mustSQL("108_inventory_purchase.sql"),
	})

	// 109：采购单与入库 7 个权限点 + 超管策略（issue #18）。
	// 条件只看本票自己的权限点（inventory:purchase_%），与 100 的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "109-inventory-purchase-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 7 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:purchase_list', 'inventory:purchase_get', 'inventory:purchase_create', " +
			"'inventory:purchase_update', 'inventory:purchase_receipt', " +
			"'inventory:purchase_production', 'inventory:purchase_history')",
		SQL: mustSQL("109_inventory_purchase_permissions.sql"),
	})

	// 110：采购入库后台菜单（issue #18）。须在 101（库存管理菜单）之后执行，
	// 且与它同挂「站点工程」目录（sort 10，排在货源管理 sort 9 之后）。
	registerSeed(Seed{
		Version:      "110-inventory-purchase-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '采购入库' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("110_inventory_purchase_menu.sql"),
	})

	// 111：主数据变更记录表（issue #19）。一张 append-only 的字段级审计表，
	// 与库存流水职责分离（后者记数量变动，本表记字段级配置变更）。
	// 默认「表存在即跳过」在「建表成功但触发器没建成」时不安全 —— 那会让
	// append-only 静默失效，故 CheckSQL 同时核对表与触发器（都齐了才算完成；
	// 缺任意一个即重跑整段 SQL，语句全部幂等）。
	// 注意：migrator.apply 固定以 TableName 作为唯一 ? 参数调用 CheckSQL。
	register(Migration{
		Version:   "111-master-data-changes",
		TableName: "master_data_changes",
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

	// 112：主数据变更记录 4 个只读权限点 + 超管策略（issue #19）。
	// 条件只看本票自己的权限点（masterdata:change_%），与 100 的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "112-master-data-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'masterdata:change_list', 'masterdata:change_count', " +
			"'masterdata:change_entities', 'masterdata:change_entity')",
		SQL: mustSQL("112_master_data_permissions.sql"),
	})

	// 113：变更记录后台菜单（issue #19）。须在 101 之后执行，与库存管理同挂
	// 「站点工程」目录（sort 11，排在采购入库 sort 10 之后）。
	registerSeed(Seed{
		Version:      "113-master-data-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '变更记录' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("113_master_data_menu.sql"),
	})

	// 114：捆绑品选项规则（issue #20）。默认「表存在即跳过」在 products 上必然误跳过，
	// 故 CheckSQL 核对形状约束是否在位（列默认值 + 规范化 UPDATE + CHECK 三者同段执行，
	// 语句全部幂等，缺约束即整段重跑）。
	register(Migration{
		Version:   "114-product-bundle-config",
		TableName: "products",
		// 按约束定义判定（只判「存在」区分不了宽松旧定义）；
		// 物化 OID 后再 deparse 的理由同 071（避免与并发 DROP SCHEMA 争用）。
		CheckSQL: "WITH target AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint WHERE conrelid = ?::regclass AND conname = 'products_bundle_items_shape_check') " +
			"SELECT CASE WHEN (SELECT COUNT(*) FROM pg_constraint pc JOIN target t ON t.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%jsonb_typeof%') = 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("114_product_bundle_config.sql"),
	})

	// 115：捆绑品 4 个权限点 + 超管策略（issue #20）。
	// 条件只看本票自己的权限点（product:bundle_%），与 082 的宽匹配互不干扰。
	registerSeed(Seed{
		Version:   "115-product-bundle-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'product:bundle_get', 'product:bundle_set', " +
			"'product:bundle_validate', 'product:bundle_skus')",
		SQL: mustSQL("115_product_bundle_permissions.sql"),
	})

	// 116：捆绑配置后台菜单（issue #20）。与 084/113 同挂「站点工程」目录（sort 12）。
	registerSeed(Seed{
		Version:      "116-product-bundle-menu",
		TableName:    "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM sys_menus WHERE title = '捆绑配置' AND type = 2 AND deleted_at IS NULL",
		SQL:          mustSQL("116_product_bundle_menu.sql"),
	})

	// 117：商品按品牌筛选的索引（issue #21）。products 表存在即默认跳过，
	// 故 CheckSQL 核对索引是否真的在位（缺索引即整段重跑，语句幂等）。
	register(Migration{
		Version:   "117-product-brand-index",
		TableName: "products",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_products_brand_id'",
		SQL: mustSQL("117_product_brand_index.sql"),
	})

	// 118：变体属性值的 GIN 索引（issue #25）。product_variants 表存在即默认跳过，
	// 故 CheckSQL 核对索引是否真的在位（缺索引即整段重跑，语句幂等）。
	register(Migration{
		Version:   "118-product-variant-option-index",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_product_variants_option_values'",
		SQL: mustSQL("118_product_variant_option_index.sql"),
	})

	// 119：变体价格的索引（issue #28）。价格维度 EXISTS 与 MIN(price) 投影都只认启用变体，
	// 故建部分索引；CheckSQL 核对索引是否真的在位（缺索引即整段重跑，语句幂等）。
	register(Migration{
		Version:   "119-product-variant-price-index",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? " +
			"AND indexname = 'idx_product_variants_price_enabled'",
		SQL: mustSQL("119_product_variant_price_index.sql"),
	})

	// 120：商品评分独立表（issue #30 修正 #29 的「评分当商品列」）。CheckSQL 核对该表是否在位
	// （缺表即整段重跑，语句幂等；DROP COLUMN 用 IF EXISTS 保证重跑安全）。
	register(Migration{
		Version:   "120-product-rating",
		TableName: "product_ratings",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 1 THEN 1 ELSE 0 END FROM information_schema.tables " +
			"WHERE table_schema = current_schema() AND table_name = ?",
		SQL: mustSQL("120_product_rating.sql"),
	})

	// 121：去掉商品侧库存缓存（issue #32）。CheckSQL 核对两个缓存列确已不存在
	//（缺列即整段重跑，语句全部 IF EXISTS 幂等）。
	register(Migration{
		Version:   "121-drop-stock-cache",
		TableName: "product_variants",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? " +
			"AND column_name IN ('stock_total', 'stock_synced_at')",
		SQL: mustSQL("121_drop_stock_cache.sql"),
	})

	// 122：删缓存相关权限点与策略（issue #32）。CheckSQL 核对两个权限点确已不存在
	//（权限点还在即整段重跑，语句幂等）。
	register(Migration{
		Version:   "122-drop-cache-permissions",
		TableName: "inventory:cache_sync",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_permission " +
			"WHERE permission_code IN (?, 'inventory:cache_reconcile')",
		SQL: mustSQL("122_drop_cache_permissions.sql"),
	})

	// 073：把历史库的 blueprints / blueprint_versions 对齐到 model（唯一真源）。
	// CheckSQL 表达「已对齐」条件（返回 > 0 则跳过）：三个历史残留列全部消失才算完成。
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
}
