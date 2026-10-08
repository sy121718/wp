package pageenums

// page_admin_enums.go - page 模块后台页面（页面列表 / 翻译工作台）的消息 key。
//
// 值是 sys_i18n 的词条 key（缺词条时由调用方给中文兜底），从 dashboard enums
// 原样搬入：词条 key 变更等于破坏既有翻译，必须逐字保留。

// MsgPagesTitle 页面列表页标题。
const MsgPagesTitle = "MsgPagesTitle" // 页面管理

// MsgPageTranslationsTitle 翻译工作台页面标题（多语言 P5c）。
const MsgPageTranslationsTitle = "MsgPageTranslationsTitle" // 多语言

// MsgFieldRequired 必填字段缺失统一提示。
const MsgFieldRequired = "MsgFieldRequired" // 必填字段不能为空

// MsgTranslationSaveFailed 译文写入失败统一提示（详情只记日志）。
const MsgTranslationSaveFailed = "MsgTranslationSaveFailed" // 译文保存失败，请稍后重试

// MsgTranslationInvalid 工作台提交数据不完整（表单被裁剪/篡改）。
const MsgTranslationInvalid = "MsgTranslationInvalid" // 提交数据不完整，请刷新页面后重试

// MsgTranslationStale 原文指纹不一致：页面草稿已变，需刷新后重填。
const MsgTranslationStale = "MsgTranslationStale" // 原文已变更，请刷新页面后重新翻译

// MsgTranslationLangInvalid 目标语言不属于站点启用语言。
const MsgTranslationLangInvalid = "MsgTranslationLangInvalid" // 目标语言未启用，请先在站点设置里启用

// MsgTranslationDocInvalid 页面草稿无法解析，无法列出可翻译文本。
const MsgTranslationDocInvalid = "MsgTranslationDocInvalid" // 页面草稿无法解析，请先在工作台修复页面

// MsgTranslationSiteScanSkipped 全站统计不可用（扫描失败），工作台退化为本页维度。
const MsgTranslationSiteScanSkipped = "MsgTranslationSiteScanSkipped" // 全站统计暂不可用，当前仅显示本页维度

// MsgTranslationSiteScanTooMany 全站页面数超过扫描上限，跳过全站统计。
const MsgTranslationSiteScanTooMany = "MsgTranslationSiteScanTooMany" // 页面数超过全站扫描上限，当前仅显示本页维度

// —— 点分 key 常量（新式）——
//
// 值是 sys_i18n 的 item_key（文案真源在迁移 451），命名按「去掉模块子域前缀
// （`admin.page_translations.`）后的语义路径」。
//
// 与上面那批 `MsgXxx = "MsgXxx"` 分开成组：老式形态的值就是常量名本身
// （`sys_i18n` 里存同名 key），两者混在同一前缀下会让人以为值也是 `MsgXxx`。
// 中文兜底留在调用点（词条缺失时的回落），不在这里。
const (
	// ReuseMoreSuffix 跨页面复用提示的省略后缀（列出上限之外还有页面时接在后头）。
	ReuseMoreSuffix = "admin.page_translations.reuse.moreSuffix" // 「 等页面」
	// ReusePathSeparator 复用提示里页面路径之间的分隔符（中文「、」/ 英文「, 」）。
	ReusePathSeparator = "admin.page_translations.reuse.pathSeparator" // 「、」
)

// —— 重定向管理页的错误码（`admin.redirect.err.*`）——
//
// 业务错误经 redirectErrKey **映射成 key**，再由 redirectErrText 取当前语言文案渲染进
// 整页提示（shell.RenderJump）。中文兜底留在调用点（词条缺失时的回落），不在这里。
const (
	// RedirectErrOccupied 目标路径已被占用（页面路径或既有重定向）。
	RedirectErrOccupied = "admin.redirect.err.occupied"
	// RedirectErrTargetMissing 重定向目标路径不存在。
	RedirectErrTargetMissing = "admin.redirect.err.target_missing"
	// RedirectErrLoop 重定向成环。
	RedirectErrLoop = "admin.redirect.err.loop"
	// RedirectErrNotFound 重定向记录不存在。
	RedirectErrNotFound = "admin.redirect.err.not_found"
	// RedirectErrUnavailable 重定向暂不可用（前置条件不满足）。
	RedirectErrUnavailable = "admin.redirect.err.unavailable"
	// RedirectErrInvalid 请求参数或路径不合法。
	RedirectErrInvalid = "admin.redirect.err.invalid"
	// RedirectErrInternal 未归类的内部错误（原文只进日志，对外归口这一条）。
	RedirectErrInternal = "admin.redirect.err.internal"
)
