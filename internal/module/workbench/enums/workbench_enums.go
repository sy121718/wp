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

// —— 画布出口的受控短句（迁移 452 seed 中英词条）——
//
// 与上面那批「可归因长文案」的分工：长文案回答「错在哪、下一步做什么」，用于**能归因**的
// 编译失败；这一批是画布出口的**固定短句** —— 少一个参数、记录不存在、能力没装配、
// 内部序列化失败。它们原先以裸中文写在 `c.String(...)` / JSON message 里，
// 英文站点上恒为中文（画布响应体不经过 pkg/response 的翻译层，也不经过模板取词）。
//
// 取词一律经 workbench_err.go 的 workbenchShortText（key + 包内中文兜底）：
// 未登记进表 = 编码错误，会回落归口文案而不是把裸 key 写进响应。

// ErrMissingPageID 画布入口缺少页面 id（?id=）。
const ErrMissingPageID = "workbench.err.missingPageId"

// ErrPageNotFound 目标页面不存在或当前账号取不到它。
const ErrPageNotFound = "workbench.err.pageNotFound"

// ErrMissingBlockID 块画布入口缺少块 id（?block=）。
const ErrMissingBlockID = "workbench.err.missingBlockId"

// ErrBlockNotFound 目标全局块不存在。
const ErrBlockNotFound = "workbench.err.blockNotFound"

// ErrTemplateNotFound 目标内容模板不存在。
const ErrTemplateNotFound = "workbench.err.templateNotFound"

// ErrTemplateParamRequired 模板预览缺少 template 参数。
const ErrTemplateParamRequired = "workbench.err.templateParamRequired"

// ErrPreviewParamsRequired 模板预览缺少 template / entityType / entityId。
const ErrPreviewParamsRequired = "workbench.err.previewParamsRequired"

// ErrPreviewParamsIncomplete 草稿预览的参数不完整（id 或 draftDocument 缺一）。
const ErrPreviewParamsIncomplete = "workbench.err.previewParamsIncomplete"

// ErrPreviewEntityIDRequired 内容实体模板预览缺少样例实体 entityId（占位符 {type} 填实体类型）。
const ErrPreviewEntityIDRequired = "workbench.err.previewEntityIdRequired"

// ErrDraftDecodeFailed 草稿文档无法解析（请求体的 draftDocument 不是合法页面文档）。
const ErrDraftDecodeFailed = "workbench.err.draftDecodeFailed"

// ErrDraftVersionStale 草稿版本与当前版本不一致（需刷新后重试）。
const ErrDraftVersionStale = "workbench.err.draftVersionStale"

// ErrTemplateDocumentEmpty 模板文档为空（无实体模式下没有可编译的文档）。
const ErrTemplateDocumentEmpty = "workbench.err.templateDocumentEmpty"

// ErrInstanceNotFound 目标商品实例不存在。
const ErrInstanceNotFound = "workbench.err.instanceNotFound"

// ErrInstanceEditNotAssembled 实例编辑端口未装配（装配缺陷，不是用户操作问题）。
const ErrInstanceEditNotAssembled = "workbench.err.instanceEditNotAssembled"

// ErrContentTemplateEditNotAssembled 内容模板编辑端口未装配。
const ErrContentTemplateEditNotAssembled = "workbench.err.contentTemplateEditNotAssembled"

// ErrContentTemplatePreviewNotAssembled 内容模板预览端口未装配。
const ErrContentTemplatePreviewNotAssembled = "workbench.err.contentTemplatePreviewNotAssembled"

// ErrStructureTemplatePreviewNotAssembled 无实体模板预览所需的页面编译端口未装配。
const ErrStructureTemplatePreviewNotAssembled = "workbench.err.structureTemplatePreviewNotAssembled"

// ErrDraftEncodeFailed 页面草稿文档序列化失败（内部错误，原文只进日志）。
const ErrDraftEncodeFailed = "workbench.err.draftEncodeFailed"

// ErrBlockEncodeFailed 块文档序列化失败（内部错误，原文只进日志）。
const ErrBlockEncodeFailed = "workbench.err.blockEncodeFailed"

// ErrTemplateEncodeFailed 模板文档序列化失败（内部错误，原文只进日志）。
const ErrTemplateEncodeFailed = "workbench.err.templateEncodeFailed"

// ErrEditorMetaEncodeFailed 编辑器元数据序列化失败（内部错误，原文只进日志）。
const ErrEditorMetaEncodeFailed = "workbench.err.editorMetaEncodeFailed"

// ErrComponentSchemaBuildFailed 组件 schema 生成失败（内部错误，原文只进日志）。
const ErrComponentSchemaBuildFailed = "workbench.err.componentSchemaBuildFailed"

// ErrComponentSchemaEncodeFailed 组件 schema 序列化失败（内部错误，原文只进日志）。
const ErrComponentSchemaEncodeFailed = "workbench.err.componentSchemaEncodeFailed"

// ErrSaveParamsIncomplete 实例覆盖文档保存的参数不完整（前端 fetch 的 JSON 体）。
const ErrSaveParamsIncomplete = "workbench.err.saveParamsIncomplete"

// ErrDetachConfirmRequired 保存会放弃模板同步，需前端确认后重试（409 的 message）。
const ErrDetachConfirmRequired = "workbench.err.detachConfirmRequired"

// ErrDraftDocumentEmpty 草稿文档为空（保存前没有任何文档体）。
const ErrDraftDocumentEmpty = "workbench.err.draftDocumentEmpty"

// —— 画布标题与双轨状态条（迁移 452 seed 中英词条）——

// TitleEditor 无页面上下文时的编辑器标题。
const TitleEditor = "workbench.title.editor"

// TitleEditorPrefix 有草稿路径时的标题前缀（后接 DraftPath，分隔符留在词条里）。
const TitleEditorPrefix = "workbench.title.editorPrefix"

// TitleBlockPrefix 块编辑模式的标题前缀（后接块名）。
const TitleBlockPrefix = "workbench.title.blockPrefix"

// TitleTemplatePrefix 模板编辑模式的标题前缀（后接模板名）。
const TitleTemplatePrefix = "workbench.title.templatePrefix"

// TitleInstance 实例（商品）编辑模式的标题。
const TitleInstance = "workbench.title.instance"

// ModeFollowTemplate 双轨状态条：当前跟随共享模板。
const ModeFollowTemplate = "workbench.mode.followTemplate"

// ModeDocument 双轨状态条：当前是只影响本实例的独立文档。
const ModeDocument = "workbench.mode.document"
