package migrations

// registerI18nDataLayer 注册「i18n 数据层与媒体中心（055–071）」。
//
// 由 register.go 的 init() 按主题拆出：本文件只做注册，不含任何 SQL 文本，
// 注册项引用的 SQL 一律经 mustSQL 从嵌入的 .sql 取。
func registerI18nDataLayer() {
	// ---- i18n 数据层（P0：表结构 + 词条 seed，docs/06-D §13 P0/P1）----
	//
	// 055：sys_i18n 补 category/remark。sys_i18n 由 init_schema.sql 建表，
	// 默认「表存在即跳过」会误跳过，故按 category 列是否存在判定。
	register(Migration{
		Version:   "055-i18n-columns",
		TableName: "sys_i18n",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'category'",
		SQL:       mustSQL("055_i18n_columns.sql"),
	})

	// 056：sys_i18n_revision 单行资源版本号（对齐 a2 历史结构，SWR 协商用）。
	register(Migration{
		Version:   "056-i18n-revision",
		TableName: "sys_i18n_revision",
		SQL:       mustSQL("056_i18n_revision.sql"),
	})

	// 057：sys_menus 补 title_key（修现存 bug：model 已 SELECT title_key，但建表缺列）。
	// 同样按列是否存在判定，避免默认「表存在即跳过」。
	register(Migration{
		Version:   "057-sys-menus-title-key",
		TableName: "sys_menus",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'title_key'",
		SQL:       mustSQL("057_sys_menus_title_key.sql"),
	})

	// 058：enums 全量词条 seed（zh-CN 195 行 / en-US 79 行，ON CONFLICT 幂等）。
	// ConditionSQL 以 zh-CN 行数为门槛：已灌满则跳过。
	// 注意：门槛统计的是**全库** zh-CN 行数，任何存量库都远超该值 —— 也就是说这条 seed
	//       只在首次建库时真正执行，往列表里补词条不会生效。新增词条一律另起迁移，
	//       并把 ConditionSQL 限制在自己的 item_key 上（样板见 226）。
	registerSeed(Seed{
		Version:      "058-i18n-seed-enums",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 199 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN'",
		SQL:          mustSQL("058_i18n_seed_enums.sql"),
	})

	// 059：后台外壳 shell.* 词条 seed（zh-CN 25 行 / en-US 25 行，多语言 P1 第二步）。
	// ConditionSQL 以 shell.* 的 zh-CN 行数为门槛（与 058 的 195 门槛互不干扰）：
	// 已灌满则跳过；新增 shell 词条时同步调大阈值即可重跑补齐。
	registerSeed(Seed{
		Version:      "059-i18n-seed-shell",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 25 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key LIKE 'shell.%'",
		SQL:          mustSQL("059_i18n_seed_shell.sql"),
	})

	// 060：访客面组件固定文案 site.component.* 词条 seed
	// （zh-CN 13 行 / en-US 13 行，多语言 P4：构建期组件文案）。
	//
	// ConditionSQL 按**本批自己的 key 前缀**判定，不再用
	// 「site.component.% 总数 ≥ 13」这种门槛：后者会被后续批次的组件词条顺带满足，
	// 于是任何**新库**上本批都会被跳过（老库因为先跑过而看不出问题）。
	// 这个坑真实发生过 —— 176 的商品列表词条（16 行）先于 seed 阶段写入，
	// 直接把门槛顶到 17，导致 gallery/slider/nav 等 13 条词条在干净库上消失。
	registerSeed(Seed{
		Version:   "060-i18n-seed-site-components",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 13 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND (" +
			"item_key LIKE 'site.component.gallery.%' OR item_key LIKE 'site.component.slider.%' OR " +
			"item_key LIKE 'site.component.nav.%' OR item_key LIKE 'site.component.video.%' OR " +
			"item_key LIKE 'site.component.countdown.%' OR item_key LIKE 'site.component.form.%' OR " +
			"item_key LIKE 'site.component.rating.%')",
		SQL: mustSQL("060_i18n_seed_site_components.sql"),
	})

	// 061：page_artifacts 加 lang 维度（多语言上线第一阻塞项，docs/06-D §15.5 第 1 条）。
	// 唯一键 (page_id, version) → (page_id, version, lang)，同页多语言各占一行。
	// page_artifacts 由 002-init-builder-schema 创建，默认「表存在即跳过」会误跳过，
	// 故按 lang 列是否存在判定（与 047/049/054/055 同一手法）。
	register(Migration{
		Version:   "061-page-artifacts-lang",
		TableName: "page_artifacts",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'lang'",
		SQL:       mustSQL("061_page_artifacts_lang.sql"),
	})

	// 062：page_publications（页面每语言激活状态，多语言 P3，docs/06-D §15.5 第 2 条）。
	// 解掉 pages.active_path 单值导致「Publish(en-US) 取消 /zh-CN/about 激活路由」。
	// 新表，默认「表存在即跳过」检查即可。
	register(Migration{
		Version:   "062-page-publications",
		TableName: "page_publications",
		SQL:       mustSQL("062_page_publications.sql"),
	})

	// 063：page_stagings（页面每语言暂存产物，多语言 P3）。
	// 解掉 pages.staged_artifact_id 单值导致「先构建两语言再逐个发布」失败。
	register(Migration{
		Version:   "063-page-stagings",
		TableName: "page_stagings",
		SQL:       mustSQL("063_page_stagings.sql"),
	})

	// 064：project_locales（站点语言清单，多语言 P3，docs/06-D §14 D10）。
	register(Migration{
		Version:   "064-project-locales",
		TableName: "project_locales",
		SQL:       mustSQL("064_project_locales.sql"),
	})

	// 065：语言切换器容器无障碍标签词条 seed（site.component.languages.label，
	// zh-CN 1 行 / en-US 1 行，多语言 P3 前台切换器）。
	// ConditionSQL 以 site.component.languages.* 的 zh-CN 行数为门槛，
	// 与 060 的 site.component.% 门槛互不干扰（060 已灌满不会再跑）。
	registerSeed(Seed{
		Version:      "065-i18n-seed-language-switcher",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key LIKE 'site.component.languages.%'",
		SQL:          mustSQL("065_i18n_seed_language_switcher.sql"),
	})

	// 066：sys_translation 内容寻址翻译表（多语言 P5a，docs/06-D §7.3）。
	// 与 sys_i18n 分工见决策 F17：本表跟内容编辑走（构建器内联文本 + CMS 字段），
	// 按 (source_hash, context, lang) 寻址，改原文即自动失效。
	// 新表，默认「表存在即跳过」检查即可（CheckSQL 留空 = 按表存在判定）。
	register(Migration{
		Version:   "066-sys-translation",
		TableName: "sys_translation",
		SQL:       mustSQL("066_sys_translation.sql"),
	})

	// 067：媒体中心（02-B）在既有 sys_attachment 上落地四能力（不新建 media_* 三表）：
	// extra_info json→jsonb（含 NULL/脏数据兜底）、GIN 索引、generation 列、md5 去重索引。
	// sys_attachment 由 001-init-schema 创建，默认「表存在即跳过」必然误跳过，
	// 故按 generation 列是否存在判定（与 047/049/054/055/057/061 同一手法）。
	register(Migration{
		Version:   "067-media-center",
		TableName: "sys_attachment",
		CheckSQL:  "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = ? AND column_name = 'generation'",
		SQL:       mustSQL("067_media_center.sql"),
	})

	// 068：PG 特性优化第一批 —— pages 块引用表达式 GIN 索引（替代 ::text LIKE 搜 JSON）
	// + 软删除部分索引（WHERE deleted_at IS NULL）。
	// pages 由 002-init-builder-schema 创建，默认「表存在即跳过」必然误跳过，
	// 故按索引名判定（与 037/038 同一手法）。
	register(Migration{
		Version:   "068-pg-jsonb-partial-index",
		TableName: "pages",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = 'idx_pages_blockref'",
		SQL:       mustSQL("068_pg_jsonb_partial_index.sql"),
	})

	// 069：删除 6 张「设计已被取代」的空表（media_asset/_variant/_reference 与
	// global_components/_versions/_policies），并解绑保留表上指向它们的 4 个外键。
	// 本迁移是 DROP 语义，默认「表存在即跳过」正好相反，故用自定义 CheckSQL：
	// 6 张表全部不存在且 4 个外键全部已解绑时返回 1（跳过），否则执行（DROP IF EXISTS 幂等）。
	// TableName 取 media_asset 作为首个 ? 参数。
	register(Migration{
		Version:   "069-drop-obsolete-design-tables",
		TableName: "media_asset",
		CheckSQL: "SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() " +
			"AND table_name IN (?, 'media_asset_variant', 'media_reference', 'global_components', " +
			"'global_component_versions', 'global_component_policies')) = 0 " +
			"AND (SELECT COUNT(*) FROM pg_constraint WHERE conname IN (" +
			"'page_component_pins_component_id_fkey', 'page_component_pins_pinned_version_id_fkey', " +
			"'content_template_component_pins_component_id_fkey', 'content_template_component_pins_pinned_version_id_fkey'" +
			")) = 0 THEN 1 ELSE 0 END",
		SQL: mustSQL("069_drop_obsolete_design_tables.sql"),
	})

	// 070：PG 优化第二梯队 —— sys_* 软删除/状态列索引核对（第一批 068 的遗留项 ①）。
	// 逐个核对 model 里真实出现的 Where 子句后，只补 3 处「查询真的会用到、且现有索引
	// 没覆盖」的缺口（全部在媒体中心）：
	//   · idx_att_file_path_alive  —— 构建期按 file_path 反查附件（此前无任何索引）
	//   · idx_mva_file_path        —— 构建期按 file_path 反查变体（此前无任何索引）
	//   · idx_att_cat_time_alive   —— 媒体库按分类分页列表（等值列 + 排序列 + 存活谓词）
	// 其余 9 张 sys_* 表要么已有覆盖索引、要么查询根本不用该条件，逐个跳过（原因写在 SQL 里）。
	// sys_attachment / sys_media_variant 早已存在，默认「表存在即跳过」必然误跳过，
	// 故按索引名判定（与 037/038/068 同一手法）。
	// 幂等检查要求 3 个索引全部存在（缺任意一个即重跑，CREATE INDEX IF NOT EXISTS 安全）：
	// 只按其中一个判定会在「部分索引被手工删除」时误跳过。
	register(Migration{
		Version:   "070-sys-status-index-audit",
		TableName: "sys_attachment",
		// 注意：migrator.apply 固定以 TableName 作为唯一 ? 参数调用 CheckSQL，
		// 故这里必须保留恰好一个 ?（用 IN (?, 'sys_media_variant') 覆盖两张表）。
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 3 THEN 1 ELSE 0 END FROM pg_indexes " +
			"WHERE schemaname = current_schema() " +
			"AND indexname IN ('idx_att_file_path_alive', 'idx_mva_file_path', 'idx_att_cat_time_alive') " +
			"AND tablename IN (?, 'sys_media_variant')",
		SQL: mustSQL("070_sys_status_index_audit.sql"),
	})

	// 071：依赖 fan-out 的反查索引 + dependency_kind 约束扩展（PIPE-3）。
	// 两表此前无任何 Go 侧写入路径，失效标记退化为全站 UPDATE；PIPE-3 起按
	// (dependency_kind, dependency_key) 反查受影响的具体产物/页面。
	//   ① idx_page_deps_lookup / idx_pres_deps_lookup —— 主键前导列是 artifact_id，
	//      反查方向（kind+key → artifact）无索引可用，必然全表扫。
	//   ② CHECK 约束补 'i18n' / 'block' 两个实现中真实存在的构建期依赖类型
	//      （Manifest 已在用 i18n；块内联进产物）。
	// 默认「表存在即跳过」对这两张早已存在的表必然误跳过，故按索引名 + 约束定义判定：
	// 2 个索引全部存在且 2 个约束都已含 'i18n' 才算完成（部分完成时重跑，语句幂等）。
	// 注意：migrator.apply 固定以 TableName 作为唯一 ? 参数调用 CheckSQL。
	register(Migration{
		Version:   "071-dependency-fanout",
		TableName: "page_dependencies",
		// 约束判定先用 MATERIALIZED CTE 锁定本迁移自己的约束 OID 再 deparse：
		// pg_get_constraintdef 会打开约束所属的关系，若在同一查询里对整个
		// pg_constraint 调用它，会与「其它测试包并发 DROP SCHEMA」竞争（实测
		// 报 "could not open relation with OID ..."）。物化出目标 OID 后，
		// 函数只对本迁移的两张表求值，不再触碰别人的关系。
		CheckSQL: "WITH target_constraints AS MATERIALIZED (" +
			"SELECT oid FROM pg_constraint " +
			"WHERE conrelid IN ('page_dependencies'::regclass, 'presentation_dependencies'::regclass) " +
			"AND conname IN ('page_dependencies_dependency_kind_check', 'presentation_dependencies_dependency_kind_check')) " +
			"SELECT CASE WHEN (" +
			"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() " +
			"AND indexname IN ('idx_page_deps_lookup', 'idx_pres_deps_lookup') " +
			"AND tablename IN (?, 'presentation_dependencies')) = 2 " +
			"AND (SELECT COUNT(*) FROM pg_constraint pc JOIN target_constraints tc ON tc.oid = pc.oid " +
			"WHERE pg_get_constraintdef(pc.oid) LIKE '%i18n%') = 2 THEN 1 ELSE 0 END",
		SQL: mustSQL("071_dependency_fanout.sql"),
	})
	// 199：project_locales RLS 工程隔离（审计 DB-009 第一批）。
	register(Migration{
		Version:   "199-rls-project-locales",
		TableName: "project_locales",
		CheckSQL:  "SELECT COUNT(*) FROM pg_policies WHERE schemaname = current_schema() AND tablename = ? AND policyname = 'project_isolation'",
		SQL:       mustSQL("199_rls_project_locales.sql"),
	})

	// 228：后台页面标题 Msg*Title 词条补缺（4 key × 2 语言，zh-CN 4 行 / en-US 4 行）。
	//
	// 为什么必须 seed：shell.Prepare 对渲染数据里的 title 做 t(title, title) —— 词条命中
	// 显示译文，未命中**回退字面量**。因此 handler 传 Msg*Title 而词条缺失时，顶栏与
	// <title> 会把 key 原样显示（"MsgMasterDataChangesTitle — go_wp 管理后台"）。
	// 2026-09 后台页面设计评审全量实测 4 个页面命中，见 SQL 头部注释。
	// 新增传 Msg*Title 的页面时，务必同批 seed 词条 —— 否则它的顶栏会直接露出 key。
	registerSeed(Seed{
		Version:      "228-i18n-seed-page-titles",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('MsgContentTemplatesTitle','MsgInventorySourcesTitle','MsgInventoryPurchasesTitle','MsgMasterDataChangesTitle')",
		SQL:          mustSQL("228_i18n_seed_page_titles.sql"),
	})

	// 229：后台页壳改造配套词条（13 key × 2 语言，zh-CN 13 行 / en-US 13 行）。
	//
	// 逐页改造（docs/02-H-admin-page-shell.md）新增的固定文案位：页头说明按钮的
	// 无障碍标签、列表空状态（标题 + 一句话）、操作列表头、筛选行「重置」。
	// 与 228 同理：模板兜底只在缺词条时显示中文，英文界面会因此回落中文，
	// 所以新 key 必须同批 seed（门禁见 admin_group_f_i18n_test.go 的双向校验）。
	registerSeed(Seed{
		Version:      "229-i18n-seed-admin-page-shell",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 13 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.common.action.reset','admin.common.field.project','admin.customers.help.label','admin.customers.list.empty_heading','admin.i18n.col.actions','admin.i18n.help.label','admin.i18n.list.empty_desc','admin.i18n.list.empty_title','admin.product_pricing.help.label','admin.product_pricing.history.note','admin.product_pricing.history.noteTail','admin.product_pricing.rules.note','admin.returns.help.label')",
		SQL:          mustSQL("229_i18n_seed_admin_page_shell.sql"),
	})

	// 230：列表页标准骨架配套词条（75 key × 2 语言，zh-CN 75 行 / en-US 75 行；
	// 原 77 个 key，2026-09 本批退役 admin.product_{categories,brands}.seo.unset 各 2 行，见 419）。
	//
	// 本轮引入的固定文案位：① 骨架通用件 —— 首列勾选框与批量条（全选 / 选中计数 /
	// 批量删除 / 行勾选无障碍标签）；② 仪表盘改为真实概览后的统计卡与最近页面列表；
	// ③ 商品分类 / 商品品牌表格化的列头、空状态与批量删除确认。
	// 与 228 / 229 同理：模板兜底只在缺词条时显示中文，英文界面会回落中文。
	// ConditionSQL 取本批 3 个代表 key 的 zh-CN 行数作门槛（不用全库计数，
	// 否则存量库永远满足、补词条永远不会执行）—— 本批退役的两个 key 不在其中，故门槛不变。
	registerSeed(Seed{
		Version:      "230-i18n-seed-list-skeleton",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.common.bulk.selectAll','admin.dashboard.stat.projects','admin.product_categories.empty.title')",
		SQL:          mustSQL("230_i18n_seed_list_skeleton.sql"),
	})

	// 231：列表页标准骨架第二轮（通用件 + 变更记录页签，20 key × 2 语言）。
	//
	// 通用件（说明按钮、操作列表头、预览动作、字数提示）已被多页复用；
	// 变更记录页把「逐条记录 / 按实体汇总」改成页签后新增了页签组与两个表格的无障碍标签。
	// 本轮其余页面的新增文案位（223 个 key）尚未翻译，清单见 docs/02-I-admin-i18n-todo.md。
	registerSeed(Seed{
		Version:      "231-i18n-seed-list-skeleton-two",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.common.help.label','admin.masterdata.view.records','admin.masterdata.label.range')",
		SQL:          mustSQL("231_i18n_seed_list_skeleton_two.sql"),
	})

	// 232：第四轮后台页面改造的全部新增文案位（223 key × 2 语言）。
	//
	// 本轮改造覆盖 55 个后台页面，产生了大量新文案位（页签组、空态标题、筛选标签、
	// 行内动作、危险操作确认……）。模板兜底只在缺词条时生效，英文界面会回落中文，
	// 故成对 seed。逐条中文对照见 docs/02-I-admin-i18n-todo.md。
	registerSeed(Seed{
		Version:      "232-i18n-seed-admin-pages-round4",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.products.col.name','admin.inventory.change.actionLabel','admin.mail.marketing.status.hint')",
		SQL:          mustSQL("232_i18n_seed_admin_pages_round4.sql"),
	})

	// 233：商品详情拆页配套词条（25 key × 2 语言）。
	//
	// 商品详情从列表页拆出后，详情页自身（标题 / 返回 / 各区块标题 / 各区块说明）
	// 与列表页新列头（价格区间 / 分类 / 品牌 / 详情 / 预览详情页）都产生了新文案位。
	registerSeed(Seed{
		Version:      "233-i18n-seed-product-detail",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 3 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key IN ('admin.product_detail.title','admin.products.col.categories','admin.products.row.detail')",
		SQL:          mustSQL("233_i18n_seed_product_detail.sql"),
	})

	// 235：仪表盘降级提示（1 key × 2 语言）。
	//
	// 仪表盘改为真实概览后，数据源读取失败不再整页 500，而是渲染页面壳 + 一条提示。
	registerSeed(Seed{
		Version:      "235-i18n-seed-dashboard-degrade",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'admin.dashboard.loadError'",
		SQL:          mustSQL("235_i18n_seed_dashboard_degrade.sql"),
	})

	// 237：block 模块补漏 —— ErrBlockProjectRequired（1 key × 2 语言）。
	//
	// DB-009 第六批给 block 加了该哨兵，但词条没同批 seed；缺词条时它会被原样显示成
	// ErrBlockProjectRequired（enums 常量的值就是 key）。判定只看自己的 key。
	registerSeed(Seed{
		Version:      "237-i18n-seed-block-project-required",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrBlockProjectRequired'",
		SQL:          mustSQL("237_i18n_seed_block_project_required.sql"),
	})

	// 253：变体清单「预览—保存」模型的词条（docs/14 §8，2026-09-19 用户拍板）——
	// 「保存」时服务端重算清单行得出的两条业务错误（ErrVariantSKUEmpty /
	// ErrVariantOptionsInvalid）+ 四个跳过原因（VariantSkip*：仍有库存 / 被 BOM 引用 /
	// 行重复 / 变体已被别处删除），加上商品详情页清单区块与生成抽屉的新文案位。
	// enums 常量值即 i18n key，不 seed 就会在页面上原样显示裸 key；判定只看自己的 key。
	registerSeed(Seed{
		Version:      "253-i18n-seed-product-variant-list",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrVariantSKUEmpty'",
		SQL:          mustSQL("253_product_variant_list_i18n.sql"),
	})

	// 264：商品侧第一批（仓库侧裸码 / 多仓与认领复用 / 无限库存 / 主体 SKU 唯一性预检）的词条。
	// 一条业务错误（ErrContainerSKUTaken，enums 常量值即 key）+ 商品列表库存三态与新建抽屉
	// 的新文案位（多仓勾选 / SKU 认领与新建预览 / 数量不填 = 无限）。判定只看自己的 key。
	registerSeed(Seed{
		Version:      "264-i18n-seed-product-warehouse-stock",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrContainerSKUTaken'",
		SQL:          mustSQL("264_product_warehouse_stock_i18n.sql"),
	})

	// 258：ErrAuditFailed（publication 模块）词条 —— 修 CQ-009 的连带项。
	// SEO 体检接口此前把 service 错误原文交给 response.ErrorWithMessage（基础设施错误会带
	// 产物路径甚至 SQL 片段）；改成固定归口 key 之后必须有词条，否则 translate 查不到会原样
	// 返回 ErrAuditFailed 这个裸 key。判定只看自己的 key。
	registerSeed(Seed{
		Version:      "258-i18n-seed-publication-audit-failed",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 1 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang = 'zh-CN' AND item_key = 'ErrAuditFailed'",
		SQL:          mustSQL("258_publication_audit_failed_i18n.sql"),
	})
}
