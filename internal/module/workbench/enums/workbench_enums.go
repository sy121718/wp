package workbenchenums

import "go_wp/pkg/facing"

const MsgDashboardTitle = "MsgDashboardTitle"

const MsgInternalError = "MsgInternalError"

const MsgCompileFailed = "MsgCompileFailed"

const ErrStructureTemplateFieldBinding = "workbench.err.structureTemplateFieldBinding"

const ErrPreviewFieldBindingUnsupported = "workbench.err.previewFieldBindingUnsupported"

const ErrPreviewActionTargetUnavailable = "workbench.err.previewActionTargetUnavailable"

const ErrPreviewDocumentInvalid = "workbench.err.previewDocumentInvalid"

const ErrTemplateEntityTypeMismatch = "workbench.err.templateEntityTypeMismatch"

const ErrTemplateFieldBindingInvalid = "workbench.err.templateFieldBindingInvalid"

const ErrTemplateProjectScope = "workbench.err.templateProjectScope"

const ErrInstanceNoDocument = "workbench.err.instanceNoDocument"

const ErrInstanceSaveRejected = "workbench.err.instanceSaveRejected"

const ErrMissingPageID = "workbench.err.missingPageId"

const ErrPageNotFound = "workbench.err.pageNotFound"

const ErrMissingBlockID = "workbench.err.missingBlockId"

const ErrBlockNotFound = "workbench.err.blockNotFound"

const ErrTemplateNotFound = "workbench.err.templateNotFound"

const ErrTemplateParamRequired = "workbench.err.templateParamRequired"

const ErrPreviewParamsRequired = "workbench.err.previewParamsRequired"

const ErrPreviewParamsIncomplete = "workbench.err.previewParamsIncomplete"

const ErrPreviewEntityIDRequired = "workbench.err.previewEntityIdRequired"

const ErrDraftDecodeFailed = "workbench.err.draftDecodeFailed"

const ErrDraftVersionStale = "workbench.err.draftVersionStale"

const ErrTemplateDocumentEmpty = "workbench.err.templateDocumentEmpty"

const ErrInstanceNotFound = "workbench.err.instanceNotFound"

const ErrInstanceEditNotAssembled = "workbench.err.instanceEditNotAssembled"

const ErrContentTemplateEditNotAssembled = "workbench.err.contentTemplateEditNotAssembled"

const ErrContentTemplatePreviewNotAssembled = "workbench.err.contentTemplatePreviewNotAssembled"

const ErrStructureTemplatePreviewNotAssembled = "workbench.err.structureTemplatePreviewNotAssembled"

const ErrDraftEncodeFailed = "workbench.err.draftEncodeFailed"

const ErrBlockEncodeFailed = "workbench.err.blockEncodeFailed"

const ErrTemplateEncodeFailed = "workbench.err.templateEncodeFailed"

const ErrEditorMetaEncodeFailed = "workbench.err.editorMetaEncodeFailed"

const ErrComponentSchemaBuildFailed = "workbench.err.componentSchemaBuildFailed"

const ErrComponentSchemaEncodeFailed = "workbench.err.componentSchemaEncodeFailed"

const ErrSaveParamsIncomplete = "workbench.err.saveParamsIncomplete"

const ErrDetachConfirmRequired = "workbench.err.detachConfirmRequired"

const ErrDraftDocumentEmpty = "workbench.err.draftDocumentEmpty"

const TitleEditor = "workbench.title.editor"

const TitleEditorPrefix = "workbench.title.editorPrefix"

const TitleBlockPrefix = "workbench.title.blockPrefix"

const TitleTemplatePrefix = "workbench.title.templatePrefix"

const TitleInstance = "workbench.title.instance"

const ModeFollowTemplate = "workbench.mode.followTemplate"

const ModeDocument = "workbench.mode.document"

const (
	DashboardPageKindHome     = "admin.dashboard.pageKind.home"
	DashboardPageKindPage     = "admin.dashboard.pageKind.page"
	DashboardPageKindArticle  = "admin.dashboard.pageKind.article"
	DashboardPageKindTag      = "admin.dashboard.pageKind.tag"
	DashboardPageKindArchive  = "admin.dashboard.pageKind.archive"
	DashboardPageKindSearch   = "admin.dashboard.pageKind.search"
	DashboardPageKindNotFound = "admin.dashboard.pageKind.notFound"
)

const (
	DashboardTrendGranularityPrefix = "admin.dashboard.trend.granularity."
	DashboardTrendTitlePrefix       = "admin.dashboard.trend.title."
	DashboardRangePrefix            = "admin.dashboard.range."
)

var errFallbacks = map[string]string{
	ErrPreviewActionTargetUnavailable:       "所选跳转目标当前不可用。请选中相关组件，在内容检查器的「跳转到」中重新选择可用的已发布目标，或恢复默认流程；需要保留该目标时，请先重新发布目标页面。",
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

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
