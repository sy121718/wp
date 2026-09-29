package migrations

// 452 — 工作台 / 后台开发登录 / 数据规则编辑器 / 插件 / 访问面片段端点的文案词条。
//
// 门槛判据（migrator 语义：ConditionSQL 返回 > 0 则跳过本 seed）：
// 数「**本批自己的 key**在 sys_i18n 里的 (key, lang) 行数」是否恰好 302（151 key × 2 语言）。
//
// 为什么逐条枚举而不是 LIKE 前缀：这是本仓的既有明文约定（见 register_order.go 的 272 号
// 迁移注释）——「用『全库总量』会被其它批次的行满足而静默跳过（058 踩过）；**用 LIKE 前缀
// 也会被将来新增的同前缀 key 带跑**」。两个方向都要堵：
//
//	· 前缀下**已有别的批次的行** → 计数虚高 → 本批被静默跳过（词条永远不 seed，最危险）；
//	· 前缀下**将来新增同前缀的词条** → 计数恒大于门槛 → 每次启动重跑（ON CONFLICT 不改行数，
//	  偏差永远追不平，同 076 的 TableName 指向不存在表）。
//
// 封闭 IN 列表两个方向都干净：本批少一条 → 302-2=300 → 重跑（不冲突则插回）；多一条 → >302
// 说明本批已 seed，跳过。
//
// 这 151 个 key 是从 452_i18n_workbench_admin_plugin_fragment.sql 里抽取的（条数已与
// SQL 的 VALUES 行数一半核对一致：151 = 302/2）。增删词条时两处必须同步改，否则门槛失准 ——
// 失准的方向是「重跑」而不是「静默跳过」，这是刻意选的失败方向。
func init() {
	registerSeed(Seed{
		Version:   "452-i18n-workbench-admin-plugin-fragment",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 302 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.datarules.editor.field_placeholder', 'admin.datarules.editor.field_required', 'admin.dev_login.csrf_failed', " +
			"'admin.dev_login.failed', 'admin.dev_login.loopback_only', 'admin.dev_login.session_failed', " +
			"'plugin.err.installNoFile', 'plugin.err.listFailed', 'plugin.err.moduleUnwired', " +
			"'plugin.err.packageUnreadable', 'plugin.hint.componentDefault', 'plugin.notice.installReselect', " +
			"'plugin.title.plugins', 'site.fragment.err.capability_missing', 'site.fragment.err.login_required', " +
			"'site.fragment.err.method_not_allowed', 'site.fragment.err.render_failed', 'workbench.err.blockEncodeFailed', " +
			"'workbench.err.blockNotFound', 'workbench.err.componentSchemaBuildFailed', 'workbench.err.componentSchemaEncodeFailed', " +
			"'workbench.err.contentTemplateEditNotAssembled', 'workbench.err.contentTemplatePreviewNotAssembled', " +
			"'workbench.err.detachConfirmRequired', 'workbench.err.draftDecodeFailed', 'workbench.err.draftDocumentEmpty', " +
			"'workbench.err.draftEncodeFailed', 'workbench.err.draftVersionStale', 'workbench.err.editorMetaEncodeFailed', " +
			"'workbench.err.instanceEditNotAssembled', 'workbench.err.instanceNotFound', 'workbench.err.missingBlockId', " +
			"'workbench.err.missingPageId', 'workbench.err.pageNotFound', 'workbench.err.previewEntityIdRequired', " +
			"'workbench.err.previewParamsIncomplete', 'workbench.err.previewParamsRequired', 'workbench.err.saveParamsIncomplete', " +
			"'workbench.err.structureTemplatePreviewNotAssembled', 'workbench.err.templateDocumentEmpty', " +
			"'workbench.err.templateEncodeFailed', 'workbench.err.templateNotFound', 'workbench.err.templateParamRequired', " +
			"'workbench.inspector.bp.desktop', 'workbench.inspector.bp.mobile', 'workbench.inspector.bp.tablet', " +
			"'workbench.inspector.corner.bottomLeft', 'workbench.inspector.corner.bottomRight', 'workbench.inspector.corners', " +
			"'workbench.inspector.corner.topLeft', 'workbench.inspector.corner.topRight', 'workbench.inspector.dir.bottom', " +
			"'workbench.inspector.dir.left', 'workbench.inspector.dir.right', 'workbench.inspector.dir.top', " +
			"'workbench.inspector.nav.any', 'workbench.inspector.nav.kind.footer', 'workbench.inspector.nav.kind.footerMobile', " +
			"'workbench.inspector.nav.kind.header', 'workbench.inspector.nav.kind.headerMobile', " +
			"'workbench.inspector.ph.classes', 'workbench.inspector.ph.cssDecls', 'workbench.inspector.ph.dimension', " +
			"'workbench.inspector.repeater.matched', 'workbench.inspector.repeater.mismatch', " +
			"'workbench.inspector.repeater.moveDown', 'workbench.inspector.repeater.moveUp', " +
			"'workbench.inspector.repeater.remove', 'workbench.inspector.section.advanced', " +
			"'workbench.inspector.section.background', 'workbench.inspector.section.border', " +
			"'workbench.inspector.section.content', 'workbench.inspector.section.hover', 'workbench.inspector.section.layout', " +
			"'workbench.inspector.section.motion', 'workbench.inspector.section.responsive', 'workbench.inspector.section.style', " +
			"'workbench.inspector.section.transform', 'workbench.mode.document', 'workbench.mode.followTemplate', " +
			"'workbench.outline.hiddenBadge', 'workbench.outline.hiddenHint', 'workbench.outline.lockedBadge', " +
			"'workbench.outline.lockedHint', 'workbench.outline.op.del', 'workbench.outline.op.down', " +
			"'workbench.outline.op.dup', 'workbench.outline.op.up', 'workbench.outline.toggle', " +
			"'workbench.title.blockPrefix', 'workbench.title.editor', 'workbench.title.editorPrefix', " +
			"'workbench.title.instance', 'workbench.title.templatePrefix', 'workbench.ui.a11y.breakpoints', " +
			"'workbench.ui.a11y.panelSwitch', 'workbench.ui.action.close', 'workbench.ui.action.collapse', " +
			"'workbench.ui.action.preview', 'workbench.ui.action.publish', 'workbench.ui.action.saveDraft', " +
			"'workbench.ui.bottom.immersive', 'workbench.ui.bottom.outline', 'workbench.ui.bottom.redo', " +
			"'workbench.ui.bottom.undo', 'workbench.ui.bp.desktop', 'workbench.ui.bp.desktopShort', " +
			"'workbench.ui.bp.laptop', 'workbench.ui.bp.mobile', 'workbench.ui.bp.mobileL', " +
			"'workbench.ui.bp.tablet', 'workbench.ui.brand', 'workbench.ui.canvas.title', " +
			"'workbench.ui.common.loading', 'workbench.ui.edit.ariaLabel', 'workbench.ui.edit.delete', " +
			"'workbench.ui.edit.empty', 'workbench.ui.edit.hideToggle', 'workbench.ui.edit.lockToggle', " +
			"'workbench.ui.edit.openLibrary', 'workbench.ui.edit.tabContent', 'workbench.ui.edit.tabStyle', " +
			"'workbench.ui.inspector.defaultOption', 'workbench.ui.inspector.empty', " +
			"'workbench.ui.inspector.mediaListPlaceholder', 'workbench.ui.inspector.pickMedia', " +
			"'workbench.ui.inspector.rangeHint', 'workbench.ui.inspector.rangeMax', 'workbench.ui.inspector.rangeMin', " +
			"'workbench.ui.library.heading', 'workbench.ui.library.searchPlaceholder', " +
			"'workbench.ui.library.tabBlocks', 'workbench.ui.library.tabComponents', 'workbench.ui.media.heading', " +
			"'workbench.ui.media.searchCategory', 'workbench.ui.media.searchFile', 'workbench.ui.media.upload', " +
			"'workbench.ui.nav.add', 'workbench.ui.nav.back', 'workbench.ui.nav.global', " +
			"'workbench.ui.nav.history', 'workbench.ui.nav.library', 'workbench.ui.nav.pageSettings', " +
			"'workbench.ui.nav.seo', 'workbench.ui.nav.tree', 'workbench.ui.settings.noTheme', " +
			"'workbench.ui.status.ready', 'workbench.ui.status.structureTemplate', " +
			"'workbench.ui.status.structureTemplateHint', 'workbench.ui.tree.heading', 'workbench.ui.tree.searchPlaceholder')",
		SQL: mustSQL("452_i18n_workbench_admin_plugin_fragment.sql"),
	})
}
