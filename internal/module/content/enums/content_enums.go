// Package contentenums content 模块响应消息。
package contentenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

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

// errFallbacks 错误兜底（对外文案的中文那一半）：与常量同处声明，出口只调一次 pkg/facing.FactOf。
//
// 为什么不再有控制器侧的白名单 + 逐条 switch：那是第二份真相 —— service 换措辞、或
// 某条错误改由别的模块提供时静默失配，症状是把业务文案吞成归口、或把内部错误当文案
// 透出，两边都不报错。现在「能不能对外说」由 facing.Fact 的形状判定，「说什么」由这里给。
//
// 取值与 sys_i18n 的 zh-CN 词条逐字一致（迁移 058 / 602 等）：兜底只在词条缺席时可见，
// 与词条不一致会让人以为「词条坏了」。
var errFallbacks = map[string]string{
	ErrInvalidParam:          "提交的信息不完整，请检查后重试。",
	ErrNotFound:              "这篇文章不存在，可能已被删除。",
	ErrInvalidType:           "内容类型不受支持。",
	ErrInvalidField:          "提交了不支持的字段，请刷新页面后重试。",
	ErrSlugTaken:             "这个路径（slug）已被另一条内容占用，换一个。",
	ErrDataInvalid:           "内容数据格式异常，请联系管理员。",
	ErrCollectionUnsupported: "当前内容服务未实现集合元数据契约。",
}

// Err 声明一条自带对外文案的内容业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// ErrDetail 把业务错误与**定位数据**拼成一条（key + 明细）。
//
// 明细是给操作者用的数据（哪个字段、哪个来源值），由本模块自己构造，不含表名 / SQLSTATE。
// 分隔符必须用 ASCII「: 」：pkg/facing 的复合形态判据只认这一种，写成全角「：」会让整条
// 过不了判据、被当成内部错误打成 500。
func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：页面回执、逐条原因）。
func FallbackFor(key string) string { return errFallbacks[key] }

// 批量操作的结论文案（页面回执，不是错误白名单）。
//
// 与 admin / order / product / page 的 Bulk* 同口径：值 = sys_i18n 的 item_key，
// **不带 Err / Msg 前缀** —— 这四句是文章列表页批量删除的结论文案（提示页正文），
// 由 handler 按计数拼出，不是 service 错误，因而不属于 articleFacingMessages
// 那张错误白名单。中文原文留在 inbound/http/content_page.go（写侧唯一一份）。
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
// （`sys_i18n` 里存同名 key），两者混在同一前缀下会让人以为值也是 `MsgXxx`。
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
