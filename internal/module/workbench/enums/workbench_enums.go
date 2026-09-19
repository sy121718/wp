// Package workbenchenums 工作台模块响应消息（哨兵串经后台 i18n 词表翻译）。
package workbenchenums

// MsgDashboardTitle 仪表盘页面标题。
const MsgDashboardTitle = "MsgDashboardTitle" // 仪表盘

// MsgInternalError 页面 handler 内部错误统一提示（禁止直出 err.Error() 泄露内部细节）。
const MsgInternalError = "MsgInternalError" // 系统内部错误，请稍后重试

// MsgCompileFailed 预览编译失败统一提示（编译器内部错误不外泄，仅提示用户检查配置）。
//
// **归口**语义（迁移 292 起）：它只兜「归不了因」的编译失败 —— 文档事实能说清原因的那几类
// 走下面的 workbench.err.* 可归因文案（分类见 inbound/http/workbench_err.go）。
// 一个 key 兜住所有失败时，作者拿到的「预览编译失败」既不知道错在哪，也不知道下一步做什么。
const MsgCompileFailed = "MsgCompileFailed" // 预览编译失败

// —— 预览 / 编译 422 的可归因文案（迁移 292 seed 中英词条）——
//
// 为什么值带 workbench 模块前缀：sys_i18n 的主键是 (item_key, lang)，裸名会与其它模块
// 互相顶掉词条（presentation 的 ErrRollbackTargetMiss 与 page 的同名哨兵踩过这个坑），
// 表现是两条不同来源的错误显示同一句话。
//
// 为什么每条都要写「怎么办」：这些 422 是**工作台画布上的死路**，作者除了这句话没有
// 任何别的线索；只说「失败了」等于把排查成本丢回给用户。分类判据见 workbench_err.go 的
// previewCompileFacingKey / templatePreviewFacingKey —— 全部是文档事实或错误哨兵，
// 不嗅探错误字符串：预览编译按**文档事实**分类，模板预览按模块哨兵 key 匹配。

// ErrStructureTemplateFieldBinding 结构模板（页眉 / 页脚）文档里带了字段绑定。
//
// 结构模板不是内容实体、没有样例实体，预览走 page 编译管线（不注入内容解析器），
// 绑定必然编译失败。这条文案存在的原因：同一现象在保存期会被 contenttemplate 拒绝
// （ErrFieldBindingInvalid），而**草稿预览**直接编译未保存文档，没有那道校验。
const ErrStructureTemplateFieldBinding = "workbench.err.structureTemplateFieldBinding"

// ErrPreviewFieldBindingUnsupported 页面 / 全局块草稿里带了字段绑定。
//
// 与结构模板同因不同处置：页面与块也不是实体实例，绑定只能来自脏数据或误拖入。
const ErrPreviewFieldBindingUnsupported = "workbench.err.previewFieldBindingUnsupported"

// ErrPreviewDocumentInvalid 文档本身没通过校验（组件配置不完整，作者可操作）。
const ErrPreviewDocumentInvalid = "workbench.err.previewDocumentInvalid"

// ErrTemplateEntityTypeMismatch 预览用的模板属于另一种内容类型（拿商品模板渲染文章）。
//
// 与 ErrTemplateMissing 分开：处置是「换模板」，不是「去建一套模板」。
const ErrTemplateEntityTypeMismatch = "workbench.err.templateEntityTypeMismatch"

// ErrTemplateFieldBindingInvalid 模板里的字段绑定越界（字段不在该类型数据源白名单内）。
const ErrTemplateFieldBindingInvalid = "workbench.err.templateFieldBindingInvalid"

// ErrTemplateProjectScope 预览的工程作用域缺失或不存在（多工程站点必须显式指定）。
const ErrTemplateProjectScope = "workbench.err.templateProjectScope"

// ErrInstanceNoDocument 实例编辑模式下实例既无快照也无覆盖文档（没有可编辑的底稿）。
const ErrInstanceNoDocument = "workbench.err.instanceNoDocument"

// ErrInstanceSaveRejected 实例覆盖文档保存被拒（文档校验或编译没过，线上保持不变）。
const ErrInstanceSaveRejected = "workbench.err.instanceSaveRejected"
