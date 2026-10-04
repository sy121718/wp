// 本包注册全部数据库迁移：按 Version 字符串排序执行。
//
// 各迁移通过代表性表名做幂等存在性检查；SQL 文本由 SplitStatements
// 按语句边界（含 DO $$ 块）安全拆分后逐条执行。
//
// 迁移注册体按主题拆到 register_*.go。为什么是这个形状：
//
//  1. 拆分前 170 条注册全部挤在一个 2199 行的 init() 里，新增一条要在巨函数中间
//     找位置插入，review 时 diff 也被整体淹没 —— 这是「手工维护」成本的前一半；
//  2. SQL 原先逐个写成 //go:embed + 包级变量（168 条 embed 指令 + 168 个变量），
//     现在整目录一次嵌入（//go:embed *.sql）、注册时按文件名取 —— 这是后一半。
//     于是新增一条迁移只剩两处改动：新增 NNN_*.sql、在对应主题文件里加一条
//     register/registerSeed；不再需要同步维护变量声明。
//  3. 「写了 SQL 却忘了注册」不再是静默失效（此前 195 未注册导致一批测试在缺列上
//     失败，正是这类遗漏）：TestEmbeddedSQLFilesAllRegistered 对磁盘 .sql 与注册
//     台账做双向一一对应校验。
//
// 执行顺序与注册顺序无关：All()/AllSeeds() 各自按 compareVersion 排序，
// ValidateRegistry 拒绝重复版本号。注册顺序仍按原 init() 的历史顺序逐段保留，
// 使「谁先注册」在 diff 里依然可见。
package migrations

import (
	"embed"
	"fmt"
)

//go:embed *.sql
var migrationSQLFS embed.FS

// registeredSQLFiles 记录被注册项引用过的 SQL 文件名，供一致性测试比对。
var registeredSQLFiles = map[string]struct{}{}

// mustSQL 按文件名取出嵌入的迁移 SQL。
//
// 找不到文件时 panic（发生在 init 阶段，等于启动即失败）：静默返回空 SQL 会让
// 一条迁移退化成「空执行」而程序照常启动 —— 这正是本文件最容易踩的坑。
func mustSQL(name string) string {
	data, err := migrationSQLFS.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("迁移 SQL %s 不存在（//go:embed *.sql 未包含该文件）: %v", name, err))
	}
	registeredSQLFiles[name] = struct{}{}
	return string(data)
}

// init 按主题顺序调用各注册组。
//
// 分组边界沿原 init() 的历史顺序切分（见各 register_*.go 的注释），因此每个
// 切片内的注册相对顺序与拆分前逐条一致。
func init() {
	registerCoreSchemaAndAccess()
	registerMediaAndUniqueConstraints()
	registerCatalogAndInventory()
	registerI18nDataLayer()
	registerIdentityAndMail()
	registerOrderAndSiteSlots()
	registerAnalyticsSeoAndPermissions()
	registerAdminI18nSeedsAndLatest()
	registerPluginPatrolI18n()
	registerBulkNoticeI18n()
	registerAdminRemainingTemplatesI18n()
	// 290：导航菜单页 + 工作台检查器的补漏词条（见 register_navigation_menu_i18n.go）。
	registerNavigationMenuI18n()
	// 291：结构模板后台改造的词条（内容模板列表页的生效 / 引用列 · 主题设置的结构模板下拉）。
	registerContentTemplateStructureI18n()
	// 292：工作台「预览 / 编译 422」的可归因文案（workbench.err.*，见 register_workbench_facing_i18n.go）。
	registerWorkbenchFacingI18n()
	// 293：运行时片段「登录面板」的访客文案（site.fragment.login_panel.*，见 register_fragment_login_panel_i18n.go）。
	registerFragmentLoginPanelI18n()
	// 294：文案词条页的只读权限点 i18n:view（补 178 的读侧缺口，见 register_i18n_view_permission.go）。
	registerI18nViewPermission()
	// 295：build_jobs 的任务租约、来源互斥与完成归属（审计 DB-01，见 register_build_lease.go）。
	registerBuildJobLease()
	// 297：块删除保护的引用类别词条（MsgBlockUsage*，见 register_block_usage_i18n.go）。
	registerBlockUsageI18n()
	// 298：商品标签页「命中商品」展开区的词条（审计 PERF-02，见 register_product_tag_hits_i18n.go）。
	registerProductTagHitsI18n()
	// 304：商品「相关商品」引用校验的词条（审计 DB-03 / PROD-01，见 register_product_related_i18n.go）。
	registerProductRelatedIDsI18n()
	// 305：产物表重复 manifest 列的收敛（审计 DB-02，见 register_manifest_dedup.go）。
	registerManifestDedup()
	// 307：build_jobs 待办去重键按「构建输入 + 语言 + 意图」分开（审计 ARCH-04，见 register_build_intent.go）。
	registerBuildJobIntent()
	// 308：页面发布计划冻结表（审计 I18N-01，见 register_publication_plan.go）。
	registerPagePublicationPlans()
	// 309：商品写路径的静态产物失效 outbox（审计 ARCH-01，见 register_product_outbox.go）。
	registerProductOutbox()
	// 312：商品标签跨工程引用拒绝（ErrTagCrossProject）的词条（审计 DB-03 / PROD-02，
	// 见 register_product_tag_cross_project_i18n.go）。
	registerProductTagCrossProjectI18n()
	// 314：商品列表筛选分页 + 商品编辑页的词条（见 register_product_edit_i18n.go）。
	registerProductEditI18n()
	// 315：商品详情页改只读的词条（见 register_product_readonly_i18n.go）。
	registerProductReadonlyI18n()
	// 316：操作列「预览」文案的覆盖迁移（191 已 seed 过该 key，seed 改不动，
	// 见 register_product_preview_override_i18n.go）。
	registerProductPreviewOverrideI18n()
	// 317：库存流水空态文案修正（旧文案指向不存在的入库表单，
	// 191 已 seed 过该 key，见 register_inventory_moves_empty_i18n.go）。
	registerInventoryMovesEmptyI18n()
	// 400：客户端筛选「无匹配结果」提示的两条词条（menus / departments，
	// 见 register_client_filter_empty_i18n.go）。
	// 注：399（管理域四页空态词条）由其文件自带的 func init() 自行注册，
	// 不在此处重复调用 —— 两处都调虽然被 sync.Once 兜住不重复执行，
	// 但会让「谁负责注册」出现两个真源。
	registerClientFilterEmptyI18n()
	// 460：页面定时上下线待办表（PIPE-7，见 register_page_schedule.go）。
	registerPageSchedules()
	// 484-486：系统配置表 + 地理地区树 + 语言货币字典（见 register_sys_reference.go）。
	registerSysReference()
	// 491：pages.excluded_langs（页面级语言排除，见 register_page_excluded_langs.go）。
	// 结构迁移**刻意不带 TableName**（pages 早已存在，带上会被「表存在即跳过」永远跳过）。
	registerPageExcludedLangs() // 492：语言准入（U1）与缺译报告（U2）的词条（见 register_locale_admission_i18n.go）。
	registerLocaleAdmissionI18n()
	// 493：站点设置页「语言方案说明」的文案覆盖（见 register_settings_locale_mode_hint_i18n.go）。
	registerSettingsLocaleModeHintI18n()
	// 494：语言面板「重新发布」的词条（见 register_page_langs_republish_i18n.go）。
	registerPageLangsRepublishI18n()
	// 495：页面列表页「缺译报告」入口词条（见 register_page_translation_misses_entry_i18n.go）。
	registerPageTranslationMissesEntryI18n()
	// 497：系统设置页（trade 组 + 本页词条，见 register_system_settings_page.go）。
	registerSystemSettingsPage()
	// 498：系统设置页侧栏入口（见 register_system_settings_menu.go）。
	registerSystemSettingsMenu()
	// 499：系统设置页 hint 改准（币种已接 SEO，见 register_system_settings_hint_currency.go）。
	registerSystemSettingsHintCurrency()
	// 500：trade 提示按产品口径改准（币种全局限定，见 register_system_settings_hint_trade_scope.go）。
	registerSystemSettingsHintTradeScope()
	// 503：trade 提示补「改币种不换算金额」（见 register_system_settings_hint_currency_no_conversion.go）。
	registerSystemSettingsHintCurrencyNoConversion()
	// 502：i18n 页语言标记词条（见 register_admin_i18n_option_ui_available.go）。
	registerAdminI18nOptionUIAvailable()
	// 506：page_schedules 全状态 page_id 复合索引（审计 DB-06，见 register_page_schedule_index.go）。
	// 结构变更，刻意不带 TableName（表早已存在，带上会被「表存在即跳过」永远跳过）。
	registerPageScheduleIndex()
	// 507：分区子表的工程隔离对账（审计 DB-02，见 register_partition_rls_reconcile.go）。
	// 目标是一个**动态集合**（当时库里有哪些分区），同样不能带 TableName。
	registerPartitionRLSReconcile()
	// 511：AI 供应商 / 模型配置表（见 register_ai.go）。
	registerAIProvider()
	// 512：AI 会话与事件日志（见 register_ai_session.go）。
	registerAISession()
	// 513：AI 模块词条（配置层页面 + ai.* 通用文案，见 register_ai_i18n.go）。
	registerAII18n()
	// 515：AI 模块后台菜单入口（见 register_ai_menu.go）。
	registerAIMenu()
	// 516：AI 会话页词条（见 register_ai_session_i18n.go）。
	registerAISessionI18n()
	// 517：AI 预设双 tab 与选模型弹窗词条（见 register_ai_preset_i18n.go）。
	registerAIPresetI18n()
	// 518：AI 早期「一码多路由」遗留的陈旧权限行清理（见 register_ai_permission_cleanup.go）。
	registerAIStalePermissionCleanup()
	// 519：AI 会话页「发消息」词条（见 register_ai_session_chat_i18n.go）。
	registerAISessionChatI18n()
	// 527：AI 会话页用量看板（指标卡 / 趋势 / 多维筛选 / 分页）词条（见 register_ai_session_usage_i18n.go）。
	registerAISessionUsageI18n()
	// 528：AI 会话详情抽屉的 token 口径词条（见 register_ai_session_usage_notes_i18n.go）。
	registerAISessionUsageNotesI18n()
	// 529：ai_event 补记供应商/模型标识（见 register_ai_event_provider_model.go）。
	registerAIEventProviderModel()
	// 530：列表行 token 悬浮卡的词条（见 register_ai_session_usage_hover_i18n.go）。
	registerAISessionUsageHoverI18n()
	// 531：折线图图例「其他」的词条（见 register_ai_session_trend_other_i18n.go）。
	registerAISessionTrendOtherI18n()
	// 532：AI 菜单合并为「大模型管理」（见 register_ai_menu_merge.go）。
	registerAIMenuMerge()
	// 533：统一入口的页头说明词条（见 register_ai_lead_i18n.go）。
	registerAILeadI18n()
	// 534：532 的收口（见 register_ai_menu_merge_followup.go）。
	registerAIMenuMergeFollowup()
	// 535：ai_event 补记 user_id（见 register_ai_event_user.go）。
	registerAIEventUser()
	// 536：AI 调用第一关卡的词条 ai.err.userRequired（见 register_ai_user_required_i18n.go）。
	registerAIUserRequiredI18n()
	// 537：大模型调用流水表 ai_call_log（见 register_ai_call_log.go）。
	registerAICallLog()
	// 538：会话行悬浮卡「最近调用」的词条（见 register_ai_session_calls_i18n.go）。
	registerAISessionCallsI18n()
	// 539：AI 会话「工具调用」的词条（见 register_ai_session_tools_i18n.go）。
	registerAISessionToolsI18n()
	// 540：概览页 KPI / 趋势 / 热销商品的词条（见 register_dashboard_overview_i18n.go）。
	registerDashboardOverviewI18n()
	// 541：模型工具调用流水表 ai_tool_call_log（见 register_ai_tool_call_log.go）。
	registerAIToolCallLog()
	// 542：对外访问令牌表 ai_access_token（见 register_ai_access_token.go）。
	registerAIAccessToken()
	// 543：对外访问令牌的词条（见 register_ai_token_i18n.go）。
	registerAITokenI18n()
	// 544：「MCP 与外部访问」菜单项（见 register_ai_mcp_menu.go）。
	registerAIMcpMenu()
	// 545：MCP 与外部访问页的词条（见 register_ai_mcp_i18n.go）。
	registerAIMcpI18n()
	// 546：MCP 与外部访问页新增的一处文案（见 register_ai_mcp_i18n_auth.go）。
	registerAIMcpI18nAuth()
	// 547：会话详情区的词条（见 register_ai_session_detail_i18n.go）。
	registerAISessionDetailI18n()
	// 548：AI 模块的全局开关组（见 register_ai_config_group.go）。
	registerAIConfigGroup()
	// 549：对外接入点开关的词条（见 register_ai_mcp_switch_i18n.go）。
	registerAIMcpSwitchI18n()
	// 550：概览页时间筛选条与区间口径的词条（见 register_dashboard_range_i18n.go）。
	registerDashboardRangeI18n()
	// 551：概览页新增两格 KPI 的词条（见 register_dashboard_kpi_i18n.go）。
	registerDashboardKpiI18n()
	// 552：概览页图表双 Tab 的词条（见 register_dashboard_chart_tabs_i18n.go）。
	registerDashboardChartTabsI18n()
	// 520：后台整页标题词条（见 register_page_title_i18n.go）。
	registerPageTitleI18n()
}
