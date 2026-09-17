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
