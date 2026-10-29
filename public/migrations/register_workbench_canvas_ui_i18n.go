package migrations

// 本批只检查明确列出的 12 个词条在两种语言中的 24 行。
func init() {
	registerSeed(Seed{
		Version:      "676-workbench-canvas-ui-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 24 THEN 1 ELSE 0 END FROM sys_i18n WHERE lang IN ('zh-CN', 'en-US') AND item_key IN ('workbench.ui.action.collapsePreview','workbench.ui.canvas.directHint','workbench.ui.canvas.page','workbench.ui.canvas.responsive','workbench.ui.edit.editing','workbench.ui.edit.restorePanel','workbench.ui.edit.tabLayout','workbench.ui.edit.tabMotion','workbench.ui.library.categories','workbench.ui.nav.pageMenu','workbench.ui.tree.fold','workbench.ui.tree.pageStructure')",
		SQL:          mustSQL("676_workbench_canvas_ui_i18n.sql"),
	})
}
