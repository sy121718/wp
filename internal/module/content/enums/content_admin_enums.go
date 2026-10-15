// content_admin_enums.go — content 模块后台文章页（列表 / 编辑 / 详情页预览 / 导入画布 /
// 译文工作台 / 发布区块）槽位事实的 i18n 词条 key。
//
// 口径与 page 模块的 page_admin_enums.go 一致：
//
//	· 值是 sys_i18n 的 item_key（缺词条时的中文兜底留在调用点 —— 控制器里的
//	  articleFacingMessages / articlePublishFacingMessages 这类兜底表）；
//	· 命名按「去掉模块子域前缀（`admin.article.`）后的语义路径」，包名 contentenums
//	  已给出模块上下文；
//	· 与 content_enums.go 顶部那批 `MsgXxx = "MsgXxx"` 分开成组：老式形态的值就是常量名
//	  本身（sys_i18n 里存同名 key）。`ErrMissingID` 这类名字里的 Err 是**语义路径的一段**
//	  （key 里真的有 `err.`），不是老式形态 —— 两者的区别在值，不在名字。
//
// 为什么这些原本写在控制器里的字面量必须提到这里：词条改名时 Go 侧没有编译期保护，
// 字面量静默失配的表现是「英文界面回落成中文」，不报错也不记日志；
// 门禁 scripts/check-controller-i18n-literals.sh（internal/module/<模块>/inbound/http/*.go
// 里出现字面量即失败，基线只减不增）。
package contentenums

const (
	// —— 文章列表 / 编辑页（content_page.go）——
	//
	// 成功回执（原 articleCreatedText / articleSavedText / articleDeletedText 的值）。
	OkCreated = "admin.article.ok.created"
	OkSaved   = "admin.article.ok.saved"
	OkDeleted = "admin.article.ok.deleted"
	// 参数级错误（原 articleMissingIDText / articleMissingTitleText / articleMissingSlugText）。
	ErrMissingID    = "admin.article.err.missingID"
	ErrMissingTitle = "admin.article.err.missingTitle"
	ErrMissingSlug  = "admin.article.err.missingSlug"

	// —— 详情页真实预览帧（ArticlePreviewFrame，GET /admin/articles/preview-frame）——
	//
	// 四条都是 iframe 里的替代页正文（Go 侧直出 HTML，不经 Jet，取词只能在控制器发生）。
	PreviewNeedSave    = "admin.article.preview.needSave"
	PreviewUnavailable = "admin.article.preview.unavailable"
	PreviewFailed      = "admin.article.preview.failed"
	PreviewEmpty       = "admin.article.preview.empty"

	// —— 导入画布（预览 / 确认创建）——
	ImportErrEmptyBody        = "admin.article.import.err.emptyBody"
	ImportErrNoProject        = "admin.article.import.err.noProject"
	ImportErrNoPath           = "admin.article.import.err.noPath"
	ImportErrNoPageCapability = "admin.article.import.err.noPageCapability"
	ImportErrDepsMissing      = "admin.article.import.err.depsMissing"
	// ImportResultCount 转换结果整句（具名占位符 {count}，Go 侧 FillTranslate 成品化）。
	ImportResultCount = "admin.article.import.resultCount"

	// 组件类型（richdoc 节点 type）的展示名，落在预览的「组件统计」列。
	ImportNodeHeading   = "admin.article.import.node.heading"
	ImportNodeText      = "admin.article.import.node.text"
	ImportNodeList      = "admin.article.import.node.list"
	ImportNodeQuote     = "admin.article.import.node.quote"
	ImportNodeImage     = "admin.article.import.node.image"
	ImportNodeDivider   = "admin.article.import.node.divider"
	ImportNodeTable     = "admin.article.import.node.table"
	ImportNodeContainer = "admin.article.import.node.container"

	// 降级动作（richdoc.WarningAction）的展示名，落在预览的「损失清单」列。
	ImportActionUnwrap      = "admin.article.import.action.unwrap"
	ImportActionDrop        = "admin.article.import.action.drop"
	ImportActionTrim        = "admin.article.import.action.trim"
	ImportActionPlaceholder = "admin.article.import.action.placeholder"

	// —— 译文工作台（article_translations.html）——
	//
	// TranslationsHeading 既是页头标题、也是回跳提示页上「立即前往」链接的文字（同一条词条）。
	TranslationsHeading   = "admin.article.translations.heading"
	TranslationsSaved     = "admin.article.translations.saved"
	TranslationsSavedNone = "admin.article.translations.savedNone"
	// 可翻译字段的展示名（contentcontract.TranslatableFields 的字段名 → 标签）。
	TranslationsFieldTitle   = "admin.article.translations.field.title"
	TranslationsFieldBody    = "admin.article.translations.field.body"
	TranslationsFieldExcerpt = "admin.article.translations.field.excerpt"
	// 提交校验与写入失败的诸条（Errors 槽位；带 {context} / {detail} 具名占位符的那几条
	// 由控制器 FillTranslate 成品化，理由是模板侧 tf 只支持 %s 位置占位符）。
	TranslationsErrPageChanged        = "admin.article.translations.err.pageChanged"
	TranslationsErrRowCountMismatch   = "admin.article.translations.err.rowCountMismatch"
	TranslationsErrStorageUnavailable = "admin.article.translations.err.storageUnavailable"
	TranslationsErrSourceChanged      = "admin.article.translations.err.sourceChanged"
	TranslationsErrSaveFailed         = "admin.article.translations.err.saveFailed"
	TranslationsErrContextInvalid     = "admin.article.translations.err.contextInvalid"
	TranslationsErrNotTranslatable    = "admin.article.translations.err.notTranslatable"
	TranslationsErrRichMismatch       = "admin.article.translations.err.richMismatch"
	TranslationsErrDepsMissing        = "admin.article.translations.err.depsMissing"
	TranslationsErrCountFailed        = "admin.article.translations.err.countFailed"
	TranslationsErrListFailed         = "admin.article.translations.err.listFailed"

	// —— 发布区块（编辑页右侧 / 新建页的不可用说明）——
	PublishOkPublished        = "admin.article.publish.ok.published"
	PublishOkRebuilt          = "admin.article.publish.ok.rebuilt"
	PublishOkURLUpdated       = "admin.article.publish.ok.urlUpdated"
	PublishErrNoProject       = "admin.article.publish.err.noProject"
	PublishErrNoURLPath       = "admin.article.publish.err.noURLPath"
	PublishErrNoTemplatePick  = "admin.article.publish.err.noTemplatePick"
	PublishHintNoTemplate     = "admin.article.publish.hint.noTemplate"
	PublishErrUnavailable     = "admin.article.publish.err.unavailable"
	PublishErrDepsMissing     = "admin.article.publish.err.depsMissing"
	PublishErrNewPathRequired = "admin.article.publish.err.newPathRequired"
	PublishHintSaveFirst      = "admin.article.publish.hint.saveFirst"
)
