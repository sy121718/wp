// Package contentenums content 模块响应消息。
package contentenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess" // 内容创建成功
	MsgUpdateSuccess = "MsgUpdateSuccess" // 内容更新成功
	MsgListSuccess   = "MsgListSuccess"   // 内容列表获取成功
	MsgDetailSuccess = "MsgDetailSuccess" // 内容详情获取成功
	MsgDeleteSuccess = "MsgDeleteSuccess" // 内容已删除
	// MsgCollectionsSuccess 集合源元数据获取成功。
	MsgCollectionsSuccess = "MsgCollectionsSuccess"

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // 内容不存在
	ErrInvalidType  = "ErrInvalidType"  // 不支持的内容类型
	ErrInvalidField = "ErrInvalidField" // 内容字段不在白名单
	ErrSlugTaken    = "ErrSlugTaken"    // 同类型下 slug 已存在
	ErrDataInvalid  = "ErrDataInvalid"  // 内容数据格式非法
	// ErrCollectionUnsupported 当前内容服务未实现集合元数据契约。
	ErrCollectionUnsupported = "ErrCollectionUnsupported"
)

// 批量操作的结论文案（页面回执，不是错误白名单）。
//
// 与 admin / order / product / page 的 Bulk* 同口径：值 = sys_i18n 的 item_key，
// **不带 Err / Msg 前缀** —— 这四句是文章列表页批量删除的结论文案（进 ?done=），
// 由 handler 按计数拼出，不是 service 错误，因而不属于 articleFacingMessages
// 那张错误白名单。中文原文留在 inbound/http/article_handle.go（写读共用同一份）。
const (
	BulkArticleNoneSelected = "content.bulk.noneSelected"
	BulkArticleAllDeleted   = "content.bulk.allDeleted"
	BulkArticleAllSkipped   = "content.bulk.allSkipped"
	BulkArticlePartial      = "content.bulk.partial"
)

// —— 点分 key 常量（新式）——
//
// 值是 sys_i18n 的 item_key（文案真源在迁移 451），命名按「去掉模块子域前缀
// （`admin.article.`）后的语义路径」：包名 contentenums 已给出模块上下文。
//
// 与上面那批 `MsgXxx = "MsgXxx"` 分开成组：老式形态的值就是常量名本身
//（`sys_i18n` 里存同名 key），两者混在同一前缀下会让人以为值也是 `MsgXxx`。
// 中文兜底留在调用点（词条缺失时的回落），不在这里。
const (
	// 文章列表页的发布状态文案。
	StatePublished   = "admin.article.state.published"   // 已发布
	StateUnpublished = "admin.article.state.unpublished" // 未发布

	// 「导入到画布」区块的不可用说明（三种前置条件各一条）。
	ImportHintSaveFirst = "admin.article.import.hint.saveFirst" // 先保存这篇文章，再回来把它导入画布。
	ImportHintNoProject = "admin.article.import.hint.noProject" // 还没有站点工程：先在「页面」里建一个工程

	// 文章 SEO 评分卡 SERP 预览的两个占位。
	ScoreSerpTitleEmpty = "admin.article.score.serpTitleEmpty" // （未填写文章标题）
	ScoreSerpDescEmpty  = "admin.article.score.serpDescEmpty"  // （未填写摘要 / SEO 描述）
)
