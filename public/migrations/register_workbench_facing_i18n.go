package migrations

import "sync"

// register_workbench_facing_i18n.go — 工作台「预览 / 编译 422」的可归因文案（迁移 292）。
//
// 单独一个主题文件：这批词条只服务于工作台画布与实例保存的 422 出口
// （结构模板 / 页面 / 全局块预览编译失败、模板预览失败、实例无文档与实例保存被拒），
// 与 register_content_template_structure_i18n.go（291，结构模板后台列表与主题设置下拉）
// 是不同主题 —— 混在一个函数里会让「这批词条是谁的」在 review 时看不出来。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册 —— 注册顺序在 diff 里可见是有意的）。
func registerWorkbenchFacingI18n() {
	registerWorkbenchFacingI18nOnce.Do(registerWorkbenchFacingI18nSeed)
}

// registerWorkbenchFacingI18nOnce 让重复调用成为空操作（不会重复 registerSeed）。
var registerWorkbenchFacingI18nOnce sync.Once

// registerWorkbenchFacingI18nSeed 注册 292 的 seed（真正干活的那一半，被 Once 包一层）。
func registerWorkbenchFacingI18nSeed() {
	// 292：8 个 key × 中英 = 16 行 + MsgCompileFailed 的 en-US 一行（共 17 行）。
	//
	// 判定枚举本批**全部 8 个 key**（COUNT(DISTINCT item_key) >= 8），不用全库行数：
	// 用全库行数会被同期其它批次的行满足而静默跳过（本仓库踩过，理由见 226/277）。
	// ConditionSQL 里没有占位符 —— 迁移器的 ? 只在 Migration.CheckSQL 上被替换成表名，
	// Seed 的 ConditionSQL 是**不带参数**执行的（db.Raw(sql)），写 ? 会直接报错 / 判定恒为 0
	//（178 踩过「恒为 0 于是每次启动重跑」的那一半）。
	//
	// 为什么要 AND 一个 EXISTS 去看那行 en：**「本批 key 齐了」不等于「本批每一行都在」**。
	// 已跑过 292 的库，8 个 zh key 早已满足门槛 → seed 直接跳过 → 后补的 en 行永远进不去
	//（ON CONFLICT DO NOTHING 让重跑安全，但前提是判定要能发现「少了一行」）。
	// 门槛（>=8）与 key 枚举保持与 SQL 一致：这次多的那个 key 只多一行 en、不新增 key。
	registerSeed(Seed{
		Version:   "292-workbench-facing-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 8 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'workbench.err.structureTemplateFieldBinding', 'workbench.err.previewFieldBindingUnsupported', " +
			"'workbench.err.previewDocumentInvalid', 'workbench.err.templateEntityTypeMismatch', " +
			"'workbench.err.templateFieldBindingInvalid', 'workbench.err.templateProjectScope', " +
			"'workbench.err.instanceNoDocument', 'workbench.err.instanceSaveRejected') " +
			"AND EXISTS (SELECT 1 FROM sys_i18n WHERE item_key = 'MsgCompileFailed' AND lang = 'en-US')",
		SQL: mustSQL("292_workbench_facing_i18n.sql"),
	})
}
