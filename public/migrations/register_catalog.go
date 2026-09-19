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

	// 104：库存变动 / 流水 / 原因字典 / 物料清单 8 个权限点 + 超管策略（issue #16）。
	// 条件只看本票自己的权限点，与 100 的宽匹配（inventory:%）互不干扰。
	//
	// 2026-09 修正：原先这里是 10 个（多 inventory:cache_sync / cache_reconcile）。那两个随
	// 库存缓存一起下线、由迁移 122 从存量库删除，但**留在本 seed 与条件里会让删除被撤回**：
	// 122 删掉 2 个 → 本条件（要求 10 个）立刻不满足 → 重新插回；而 Migrations 台账先跑、
	// Seeds 台账后跑（migrator.go 的 runAll 与 RunSeeds），终态永远是「死权限点又回来了」——
	// 后台于是存在指向不存在路由的死授权（勾选后毫无作用、误导配置者）。
	// 条件与 SQL 同批收到 8 个：**删能力时必须连 seed 一起收口**。
	// 回归判据：migrations_retired_permission_test.go（删过的码不得再被任何 seed 写入）。
	registerSeed(Seed{
		Version:   "104-inventory-change-permissions",
		TableName: "sys_permission",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 8 THEN 1 ELSE 0 END FROM sys_permission WHERE permission_code IN (" +
			"'inventory:stock_change', 'inventory:stock_deduct', 'inventory:movement_list', " +
			"'inventory:reason_list', 'inventory:reason_create', 'inventory:reason_update', " +
			"'inventory:bom_set', 'inventory:bom_get')",
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

	// 229：库存独立成一级目录，仓库与变动原因字典各自成页（2026-09 设计评审第三轮）。
	//
	// 原先「库存管理 / 货源管理 / 采购入库」混在「商品与库存」目录下，与商品八项并列 ——
	// 直接的后果是库存管理页靠 <details> 折叠把「流水 + 改库存 + 仓库配置 + 原因字典」
	// 四件事塞进一页。折叠不是解法：装不下就该拆页，并把模块边界反映到菜单上。
	// CheckSQL 以新增页 /admin/inventory/warehouses 的菜单行为门槛（整条迁移只跑一次）。
	register(Migration{
		Version:   "229-inventory-menu-split",
		TableName: "sys_menus",
		// 约定：自定义 CheckSQL 必须接收迁移器传入的表名参数（migrator_test.go 会检查）。
		// 注意 ? 是**值占位**（会被替换成 $1），只能出现在值的位置 ——
		// 写成 "FROM ?" 会得到 FROM $1 的语法错误（实测踩过）。
		CheckSQL: "SELECT COUNT(*) FROM sys_menus " +
			"WHERE path = '/admin/inventory/warehouses' AND to_regclass(?) IS NOT NULL",
		SQL: mustSQL("229_inventory_menu_split.sql"),
	})

	// 236：修正 229 给三个菜单误置的 is_public = 1（库存目录 / 仓库管理 / 变动原因字典）。
	//
	// 229 把这三个当成了「登录即可见」，但它们是有权限点的业务页面 ——
	// 结果是任何登录用户在侧栏都看得到、点进去却 403（「看得到点不了」）。
	// 224 的约定是只有「无子节点的直接链接」（仪表盘）才 is_public = 1；
	// 目录的可见性由授权树按「有可见子孙」自动补齐，不需要这个开关。
	// CheckSQL 判「这三条是否都已归零」：已修则跳过（幂等）。
	register(Migration{
		Version:   "236-fix-inventory-menu-visibility",
		TableName: "sys_menus",
		// 迁移器的约定：CheckSQL **返回 0 = 执行、非零 = 跳过**（见 229 的写法 ——
		// 菜单行已存在返回 1 即跳过）。所以这里「还有没归零的」要返回 0。
		CheckSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 0 ELSE 1 END FROM sys_menus " +
			"WHERE to_regclass(?) IS NOT NULL AND deleted_at IS NULL AND is_public = 1 AND (" +
			"path IN ('/admin/inventory/warehouses','/admin/inventory/reasons') " +
			"OR (type = 1 AND parent_id = 0 AND title = '库存'))",
		SQL: mustSQL("236_fix_inventory_menu_visibility.sql"),
	})

	// 238：商品类型列（variant / bundle）+ 存量捆绑品回填。
	// CheckSQL 判列是否存在：已加则返回非零跳过（约定见 236）。
	register(Migration{
		Version:   "238-product-type",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'type'",
		SQL: mustSQL("238_product_type.sql"),
	})

	// 239：新错误词条（商品类型 / 捆绑容器价）+ 新建商品抽屉的「商品类型」文案。
	// 判定只看自己的 key（058 那种全库计数在存量库永远判定已灌满）。
	registerSeed(Seed{
		Version:      "239-i18n-seed-product-type",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrProductTypeInvalid'",
		SQL:          mustSQL("239_i18n_seed_product_type.sql"),
	})

	// 240：仓库类型（self / third_party / virtual）与第三方对接配置 config jsonb（库存域收口）。
	//
	// 存量「is_default 那一行」按自营（self）处理，理由写在 SQL 注释里：默认仓是
	// 「未指定仓库」时的兜底，必须有实体收发能力；虚拟仓不能作为默认仓（service 同款守卫）。
	// CheckSQL 判「两列 + check 约束都在位」：只判列会在「列已加、约束没建成」时静默跳过，
	// 那样类型就不再受 DDL 兜底（DB-015 的教训）。
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

	// 241：变动原因的 name 收口为 i18n key（库存域收口）。
	//
	// 内置原因 → inventory.reason.<code>；存量自定义原因 → 原文案写进 sys_i18n 后再改成
	// inventory.reason.custom.<project_id>.<code>。
	//
	// 为什么是 registerSeed 而不是 register：它处理的是**数据**，而写数据的 103 在 seed 阶段。
	// 结构迁移阶段（Run）执行时表还是空的 —— 判定返回「没有需要处理的行」直接跳过，
	// 随后 103 才把中文名插进去，新库的 name 就永远停在旧形态上（老库因为早有数据反而正常）。
	// 注册在 103 之后，RunSeeds 按注册顺序执行，两个阶段的库都收口。
	// CheckSQL 的 ? 由迁移器传入表名（约定见 229 / migrator_test.go）；
	// 判定「还有没 key 化的行」：重跑时条件不再成立，不会覆盖已经写好的词条。
	registerSeed(Seed{
		Version:   "241-inventory-reason-i18n-key",
		TableName: "inventory_change_reasons",
		// ConditionSQL 里**不能出现 ?**：seed 的检查走 db.Raw(sql) 且不传参（Migration 的 apply
		// 不同：它只在 SQL 含 ? 时才传表名）。多写一个占位符会让整条 seed 直接报
		// "expected 0 arguments, got 1"，每次启动都失败、原因还指向检查语句。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM inventory_change_reasons " +
			"WHERE name <> '' AND name NOT LIKE 'inventory.reason.%'",
		SQL: mustSQL("241_inventory_reason_i18n_key.sql"),
	})

	// 242：库存域错误词条 + 内置变动原因词条（中英成对）。
	// 判定只看自己的 key：库存域此前从未 seed 过错误词条，页面一旦不再直出 err.Error()，
	// 缺词条就会显示 ErrWarehouseNotFound 这种裸 key。
	registerSeed(Seed{
		Version:      "242-inventory-i18n-seed",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'inventory.reason.purchase_in'",
		SQL:          mustSQL("242_inventory_reason_i18n.sql"),
	})

	// 243：库存三个后台页的新文案词条（调整入口 / 时间筛选 / 仓库类型与第三方配置 / 原因启停）。
	registerSeed(Seed{
		Version:      "243-inventory-page-i18n-seed",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.inventory.adjust.title'",
		SQL:          mustSQL("243_i18n_seed_inventory_pages.sql"),
	})

	// 244：仓库侧成本（inventory_stocks.cost_price）+ 仓库内 SKU 唯一（批次 A）。
	//
	// 口径见 docs/14 §4（用户 2026-09-19 确认）：成本写到**仓库侧**、只记一个当前值
	//（不做成本流水），(仓库, SKU) 与库存同维度；NULL = 尚未核算，0 是合法的显式成本。
	// 仓库内唯一（UNIQUE (warehouse_id, sku_code)）在建约束**之前**先扫存量重复：
	// 有重复就带样例 RAISE EXCEPTION（迁移失败、数据不动），绝不静默丢数据、也不留一个
	// 没有上下文的 23505。CheckSQL 判「列 + 约束都在位」—— 只判列会在「列已加、约束没建成」
	// 时静默跳过，那样仓库内唯一就不再受 DDL 兜底（DB-015 的教训，与 240 同一手法）。
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

	// 245：仓库侧成本的词条（批次 A）—— 新增的 ErrStockCostInvalid 与「未核算」展示兜底。
	// 判定只看自己的 key（同 239/247/248）。
	registerSeed(Seed{
		Version:      "245-i18n-seed-inventory-cost",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrStockCostInvalid'",
		SQL:          mustSQL("245_i18n_seed_inventory_cost.sql"),
	})

	// 246：商品的容器主体 SKU（products.sku_code）+ 项目内唯一偏索引。
	//
	// 新 SKU 规则下「容器主体」是商品的对外身份（变体商品 = <仓短码>_<仓库里那条 SKU>；
	// 捆绑 = 自定义、以 _B 结尾），而 products 此前**没有 SKU 列** —— 捆绑自 238 起不再生成
	// 首个变体，主体 SKU 无处可放；变体商品的「主体」同样没有落点（变体行只存变体自己的 SKU）。
	// 唯一性口径见 docs/14 §4：主体唯一（本索引）/ 变体不额外收紧 / 捆绑成员不校验。
	// CheckSQL 判列是否存在：已加则跳过（约定见 236）。
	register(Migration{
		Version:   "246-product-container-sku",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'sku_code'",
		SQL: mustSQL("246_product_container_sku.sql"),
	})

	// 247：商品域后续词条（捆绑构成区块 19 / 多语言空态 4 / 新建抽屉属性组多选与批量改价 6）
	// + 退役旧的 admin.products.ph.attributeIds（属性引用文本框时代的占位符，改版后无模板取用）。
	// 判定只看自己的 key（058 那种全库计数在存量库永远判定已灌满）。
	registerSeed(Seed{
		Version:      "247-i18n-seed-product-followups",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.product_detail.bundle.title'",
		SQL:          mustSQL("247_i18n_seed_product_followups.sql"),
	})

	// 248：商品主体 SKU（2026-09-19 评审第四轮）的词条 ——
	// ErrSkuContainerMissing / ErrContainerSkuInvalid 两条业务错误（enums 常量值即 i18n key，
	// 不 seed 就会在页面上原样显示裸 key），加上新建抽屉的「SKU 编码」字段文案与详情页
	// 主体 SKU 的唯一性说明（三个模板 key）。判定只看自己的 key（同 239/247）。
	registerSeed(Seed{
		Version:      "248-i18n-seed-product-sku",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrSkuContainerMissing'",
		SQL:          mustSQL("248_product_sku_error_i18n.sql"),
	})

	// 249：捆绑商品主体 SKU 必填（2026-09-19 用户拍板）的词条 ——
	// ErrBundleSKURequired 一条业务错误（常量值即 i18n key，不 seed 就会在页面上原样显示裸 key），
	// 加上新建抽屉的四个文案位（「重新生成」按钮 / 建议值说明 / 请手填提示 / 捆绑占位符）。
	// 变体商品一字未动，248 的三条 key 继续有效、不退役。判定只看自己的 key（同 239/247/248）。
	registerSeed(Seed{
		Version:      "249-i18n-seed-product-bundle-sku",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrBundleSKURequired'",
		SQL:          mustSQL("249_product_bundle_sku_i18n.sql"),
	})

	// 250：商品目录菜单归属收口（2026-09-19 评审第四轮）——
	//   定价工具 / 捆绑配置 下线（status = 0），商品详情模板从「商品与库存」改挂「内容」。
	// 这里必须是 seed 而不是 Migration：这三行菜单由 097 / 116（seed）与 224（seed）建出来，
	// 而 migrations.Run 先于 RunSeeds —— 按 Migration 注册会在干净库上判定「无菜单可改」而跳过，
	// 新装环境拿不到归位（229 能按 Migration 写是因为它自己建菜单）。
	// ConditionSQL 表达「已无待修项」：两处下线都已归零、且模板菜单的父级已是「内容」；
	// 缺「内容」目录时 EXISTS 为假 → 判为「无需修」跳过（该场景下 UPDATE 本身也不会命中，语义一致）。
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

	// 251：仓库侧外部编码（inventory_stocks.external_sku）+ 按外码反查的普通索引。
	//
	// 口径见 docs/14 §9.3（2026-09-19 用户补充确认）：属性属于商品，仓库侧只回答
	// 「这条货在这个仓叫什么」—— 本列就是那个「叫什么」。映射是 **N:1**
	//（同一个商品的十几个口味在仓库侧共用同一条 SKU / 同一个价格），
	// 所以**只建普通索引、不建唯一索引**：UNIQUE (warehouse_id, external_sku)
	// 会把「多口味共用一个外码」这种合法数据判成冲突。
	// 唯一性改由 service 弱校验（同一仓内同一外码必须指向同一个 product_id），
	// UNIQUE (warehouse_id, sku_code)（我们自己的 SKU 仓内唯一，迁移 244）保持不动。
	// CheckSQL 判「列 + 索引都在位」：只判列会在「列已加、索引没建成」时静默跳过，
	// 那样按外码反查就退化成全表扫描（DB-015 的教训，与 240/244 同一手法）。
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

	// 252：仓库 SKU 外部编码的词条（迁移 251 配套）——
	// 7 条库存域业务错误（常量值即 i18n key，不 seed 就会在页面上原样显示裸 key）
	// 加 10 个模板文案位（新建商品抽屉的「SKU 来源」整块 9 个 + 库存页「外部编码」列 1 个）。
	// 判定只看自己的 key（同 239/247/248/249）。
	registerSeed(Seed{
		Version:      "252-i18n-seed-inventory-external-sku",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrExternalSKUProductConflict'",
		SQL:          mustSQL("252_i18n_seed_inventory_external_sku.sql"),
	})

	// 255：库存页「外部编码」行内编辑的文案位（迁移 251 的字段终于有了可编辑入口）——
	// 三个模板文案位（占位符 / 保存按钮 / 按钮说明），中英各一行。
	// 业务错误的词条（ErrExternalSKUInvalid / ErrExternalSKUProductConflict）在 252 已 seed，
	// 本批不重复；本批也没有新增 enums 常量。
	//
	// 为什么是 seed 而不是 Migration：它只往 sys_i18n 写词条，与 242/243/252 同性质 ——
	// seed 可重复执行，且后台改过的文案不会被覆盖（ON CONFLICT DO NOTHING）。
	// 判定只看自己的 key：count >= 1 即视为已 seed（同 239/247/248/249/252）。
	registerSeed(Seed{
		Version:      "255-i18n-seed-inventory-external-sku-edit",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.inventory.sku.externalSku.save'",
		SQL:          mustSQL("255_inventory_external_sku_edit_i18n.sql"),
	})

	// 259：捆绑成员的三种来源 + 变体删除守卫补引用面（docs/14 §1.2 / §8，批次 C）——
	// 两类 enums 常量（常量值即 i18n key，不 seed 就原样返回裸 key）：
	//   · 删除守卫的两个新引用面：被捆绑成员引用 / 有过库存流水；
	//   · 成员来源（BundleSource*）与解析期错误 / 逐条跳过原因（ErrBundleSource*、BundleMember*）；
	// 加两类模板文案：捆绑配置页的成员来源面板、商品详情页「捆绑构成」的来源列。
	// 判定只看自己的 key：count >= 1 即视为已 seed（同 239/247/248/249/252/253/255）。
	registerSeed(Seed{
		Version:      "259-i18n-seed-product-bundle-member-source",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'BundleMemberNotOnProduct'",
		SQL:          mustSQL("259_product_bundle_member_source_i18n.sql"),
	})

	// 260：products.bundle_items 的 GIN 索引（批次 C 的删除守卫查询）。
	// 为什么是结构迁移而不是「顺手加一句 DDL」：这条查询在**每次删除变体 / 保存变体清单**时
	// 都要按变体 id 判定「是否被某个捆绑引用」，没有索引就是全表扫 products 的 jsonb 列。
	// 注意查询必须写成**整列包含**（bundle_items @> …）才能命中它 —— 迁移文件注释里记了
	// 三条 EXPLAIN 实测（整列索引 + 嵌套表达式用不上）。
	// CheckSQL 判本索引是否已在：限定 current_schema()，否则并发测试或残留 schema 里的
	// 同名索引会让判定恒为真、迁移被静默跳过（167 / p7_index_audit_test 都踩过）。
	register(Migration{
		Version:   "260-products-bundle-items-gin",
		TableName: "products",
		CheckSQL: "SELECT COUNT(*) FROM pg_indexes " +
			"WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'idx_products_bundle_items_gin'",
		SQL: mustSQL("260_products_bundle_items_gin.sql"),
	})

	// 261：无限库存（不跟踪数量）开关 —— inventory_stocks.track_quantity（库存域第一批）。
	//
	// 口径（用户 2026-09-19 拍板）：无限用**显式开关**表达（false = 不跟踪 = 无限），
	// quantity 保持 NOT NULL DEFAULT 0 并加 CHECK (track_quantity OR quantity = 0)；
	// **存量行一律 track_quantity = true（保守）** —— 存量那些 0 无法区分为「建行占位」
	// 还是「卖光了」，把卖光的行判成无限会直接导致超卖（无限行扣减不校验可用量）。
	// 新建行才默认无限，理由逐条写在 SQL 注释里。
	// 存量 UPDATE 只在「本次真的新增了这一列」时执行（DO 块内判 added），
	// 重跑不会把运营手工改成无限的存量行重新掰回跟踪。
	// CheckSQL 判列是否存在：已加则跳过（约定见 236）。
	register(Migration{
		Version:   "261-inventory-stock-track-quantity",
		TableName: "inventory_stocks",
		CheckSQL: "SELECT COUNT(*) FROM information_schema.columns " +
			"WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'track_quantity'",
		SQL: mustSQL("261_inventory_stock_track_quantity.sql"),
	})

	// 262：仓库里的 SKU 永远是裸码 —— 剥掉存量库存行 sku_code 上多余的仓码前缀。
	//
	// 口径：仓库里的 SKU 不带仓码前缀（商品侧才带，前缀标注归属 / 认领仓），
	// 所以 inventory_stocks.sku_code 应当是裸码。规则 = upper(sku_code) 以
	// upper(warehouse.code) + '_' 开头则去掉这一节，只对能 JOIN 到仓库的行做。
	// **先扫描再更新**：剥掉后同一 (warehouse_id, sku_code) 出现重复时显式
	// RAISE EXCEPTION 带冲突明细，绝不静默合并两行（两行是两份事实，合并就是丢账）。
	// CheckSQL 判「是否还有带前缀的行」：没有则跳过（返回 1）；用 to_regclass(?) 锚定
	// 本迁移自己的对象，保证约定要求的表名占位符真的出现在语句里（同 229 的手法）。
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

	// 263：无限库存的新文案词条（无限 / 不跟踪 / 跟踪库存 / 未入库 / 数量提示 等），
	// 中英成对；含本批两条新业务错误（ErrStockQuantityRequired / ErrStockUntrackedQuantity，
	// 常量值即 i18n key，不 seed 就会在页面上原样显示裸 key）。
	// 判定只看自己的 key：count >= 1 即视为已 seed（同 239/247/248/249/252/255）。
	// ConditionSQL 里**不能出现 ?**：seed 的检查走 db.Raw(sql) 且不传参（同 241）。
	registerSeed(Seed{
		Version:      "263-i18n-seed-inventory-track-quantity",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.inventory.stock.unlimited'",
		SQL:          mustSQL("263_inventory_track_quantity_i18n.sql"),
	})
}
