// Package workbenchenums 工作台模块响应消息（哨兵串经后台 i18n 词表翻译）。
package workbenchenums

import "go_wp/pkg/facing"

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

// —— 仪表盘页（admin/dashboard.html）的页面级文案 key（迁移 550 / 552 / 553 / 554 seed）——
//
// key 属 `admin.` 名字空间（它们是**后台页面**的文案，不是工作台画布控件的文案），
// 取词发生在工作台的 dashboard 控制器里：模板侧同名 key 内联、控制器侧必须走常量
// （门禁 scripts/check-controller-i18n-literals.sh）。
const (
	DashboardPageKindHome     = "admin.dashboard.pageKind.home"
	DashboardPageKindPage     = "admin.dashboard.pageKind.page"
	DashboardPageKindArticle  = "admin.dashboard.pageKind.article"
	DashboardPageKindTag      = "admin.dashboard.pageKind.tag"
	DashboardPageKindArchive  = "admin.dashboard.pageKind.archive"
	DashboardPageKindSearch   = "admin.dashboard.pageKind.search"
	DashboardPageKindNotFound = "admin.dashboard.pageKind.notFound"
)

// 以下三条是**前缀**（调用方按粒度 / 区间键拼后缀），值不是完整 item_key。
const (
	DashboardTrendGranularityPrefix = "admin.dashboard.trend.granularity."
	DashboardTrendTitlePrefix       = "admin.dashboard.trend.title."
	DashboardRangePrefix            = "admin.dashboard.range."
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrStructureTemplateFieldBinding` 这样的裸 key。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
// 对账由 internal/architecture 的 error_fallback_ownership_test.go 双向守。
var errFallbacks = map[string]string{
	ErrStructureTemplateFieldBinding:        "这份页眉 / 页脚里有「字段绑定」：结构模板是站点级结构，不是某个内容实体的实例，同一份页眉在商品页与文章页会拿到不同的值，所以构建时不解析绑定。请到画布里选中带绑定的组件（标题 / 正文 / 图片 / 商品卡等），清空它的绑定槽位改用静态内容；要让某段内容随实体变化，请把那个组件放进对应的内容模板。",
	ErrPreviewFieldBindingUnsupported:       "这份文档里有「字段绑定」，但页面与全局块不是内容实体实例，编译时不解析绑定。请到画布里选中带绑定的组件，清空绑定槽位改用静态内容（或删掉该组件）；需要按实体字段显示的内容，请放进对应的内容模板。",
	ErrPreviewDocumentInvalid:               "画布文档没通过校验：多半是某个组件的配置不完整（组件必需的槽位留空 —— 例如轮播没有 slide、表单缺提交按钮、图片缺地址），或组件树嵌套超过上限。请按画布与检查器里的提示逐项补齐、把结构拍平，再重新预览。",
	ErrTemplateEntityTypeMismatch:           "这套模板是给另一种内容类型用的（例如拿商品模板渲染文章），不能这样预览 —— 模板的数据源与当前实体的字段对不上。请换成与当前类型一致的模板；要新建就到「内容模板」页按当前类型建一套。",
	ErrTemplateFieldBindingInvalid:          "模板里的字段绑定越界：绑定的字段不属于当前内容类型的数据源，或字段名已被改名 / 删除。请回到工作台检查带绑定的组件，改用本类型数据源里确实存在的字段（白名单由实体类型决定）。",
	ErrTemplateProjectScope:                 "预览的工程作用域没定下来：站点里有多个工程时必须显式指定。请从对应工程的入口（页面列表 / 商品详情）重新打开工作台，让地址里带上 projectId。",
	ErrInstanceNoDocument:                   "这个实例还没有可编辑的文档：它既没有发布过快照，也没有自己的覆盖文档。请先到它的详情页点「重新套用预设」生成初始文档，再回到工作台编辑。",
	ErrInstanceSaveRejected:                 "保存没通过：这份文档没有编译成功（组件必需槽位留空 / 字段绑定越界 / 引用的模板或块不可用都会走到这里）。你的改动没有写入，线上内容保持不变 —— 请按画布与检查器里的提示修正后重试；想回到跟随模板的状态，用详情页的「重新套用预设」。",
	ErrMissingPageID:                        "缺少页面 id",
	ErrPageNotFound:                         "页面不存在",
	ErrMissingBlockID:                       "缺少块 id",
	ErrBlockNotFound:                        "全局块不存在",
	ErrTemplateNotFound:                     "模板不存在",
	ErrTemplateParamRequired:                "缺少 template",
	ErrPreviewParamsRequired:                "缺少 template / entityType / entityId",
	ErrPreviewParamsIncomplete:              "预览参数不完整",
	ErrPreviewEntityIDRequired:              "缺少预览样例实体 entityId（字段绑定预览需要一条真实 {type} 记录）",
	ErrDraftDecodeFailed:                    "草稿文档解析失败",
	ErrDraftVersionStale:                    "草稿版本已更新，请刷新后重试",
	ErrTemplateDocumentEmpty:                "模板文档为空",
	ErrInstanceNotFound:                     "实例不存在",
	ErrInstanceEditNotAssembled:             "实例编辑能力未装配",
	ErrContentTemplateEditNotAssembled:      "内容模板编辑能力未装配",
	ErrContentTemplatePreviewNotAssembled:   "内容模板预览能力未装配",
	ErrStructureTemplatePreviewNotAssembled: "无实体模板预览能力未装配",
	ErrDraftEncodeFailed:                    "草稿文档序列化失败",
	ErrBlockEncodeFailed:                    "块文档序列化失败",
	ErrTemplateEncodeFailed:                 "模板文档序列化失败",
	ErrEditorMetaEncodeFailed:               "编辑器元数据序列化失败",
	ErrComponentSchemaBuildFailed:           "组件 schema 生成失败",
	ErrComponentSchemaEncodeFailed:          "组件 schema 序列化失败",
	ErrSaveParamsIncomplete:                 "保存参数不完整",
	ErrDetachConfirmRequired:                "这次改动会让本商品转为独立文档：之后模板更新不再同步到这里；想回到跟随时，在商品详情页点「重新套用预设」即可。继续保存？",
	ErrDraftDocumentEmpty:                   "草稿文档为空",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
