package navigationenums

// navigation_enums_translation_write.go — 译文整表写入（事务化）新增的错误声明。
//
// 为什么单独一个文件：既有 navigation_enums.go 的 errFallbacks 与常量是历史一批
// （兜底对账测试按 AST 扫全包，新增常量会被自动纳入），本批只追加**三条**新错误 +
// 两处兜底修正。独立文件让「本批新增了什么」在 diff 里一眼可见，也不与别的 owner
// 在途改动同一段。
//
// 层归属：enums 只声明 key + 中文兜底 + 状态码（facing.WithStatus），不取词、不拼句、
// 不认识任何服务端实现，也不 import 别人的 service/model。

import (
	"net/http"

	"go_wp/pkg/facing"
)

// —— 新增 key（错误 key 域：*.err.*，不得当界面文案）——

const (
	// ErrTranslationDuplicate 同一批提交里出现两条寻址相同的行（同 hash + 同语境）。
	//
	// 为什么拒绝而不是「后者覆盖前者」：两条不同的译文指向同一格数据时，
	// 「最终写哪一条」只能由提交顺序决定，而提交顺序对运营不可见 ——
	// 症状是「我填的译文被另一行盖掉了」，且日志里看不出任何冲突。
	// 拒绝时把可定位的行（hash + 两个译文）放 data.items 交给人工裁决。
	ErrTranslationDuplicate = "navigation.err.translation.duplicate" // 同一菜单文字在一批提交里出现了多次，请只保留一行

	// ErrTranslationWriteFailed 译文写入失败（数据库层的归口）。
	//
	// 与既有 ErrSaveFailed（页面错误槽文案 key）分开：那条是**页面正文**里的文案，
	// 本批写路径是纯 JSON 出口，事实必须是一条 facing 哨兵，出口才能一次取词 + 真实状态码。
	// 已知业务原因（未知 hash / 不可翻译 / 语境非法 / 重复行）各有自己的 key，
	// 走到这里的一律是「存储层失败」—— 细节（表名 / SQLSTATE）只进日志。
	ErrTranslationWriteFailed = "navigation.err.translation.writeFailed" // 译文保存失败，请稍后重试

	// ErrTranslationWriteUnavailable 写服务未装配完成（端口 / 事务能力缺失）。
	//
	// 与 ErrTranslationWriteFailed 分开的理由：这条是**装配缺陷**（重试永远不会成功），
	// 而那条是运行时故障（可能自愈）。混成一条会让运维照着「请稍后重试」去等一个
	// 直到发版才会消失的问题。
	ErrTranslationWriteUnavailable = "navigation.err.translation.unavailable" // 译文写入服务未就绪，请联系管理员
)

// translationWriteStatus 声明处给状态码（出口只调 response.ErrorStatusAuto，不再二次判断）。
const (
	// translationWriteRejectStatus 批量拒绝（可定位的数据冲突 / 不可翻译）：409。
	//
	// 为什么不可翻译是 409 而不是 400：请求形状完全合法，是**当前库内菜单状态**与
	// 提交断言不一致（这段文字本来就不参与翻译）。400 会让调用方去改请求形状，
	// 而正确的动作是刷新工作台、去掉那一行。
	translationWriteRejectStatus = http.StatusConflict
	// translationWriteFailureStatus 存储 / 装配失败：500。
	translationWriteFailureStatus = http.StatusInternalServerError
)

// ErrTranslationDuplicateError 组装「重复行」的业务错误（带 409 状态码）。
//
// 明细不在文本里：可定位的数据由 TranslationRejectedError 的 items 给
// （id = hash、name = 服务端原文、reasonKey = 本 key）。
func ErrTranslationDuplicateError() error {
	return facing.WithStatus(facing.New(ErrTranslationDuplicate, errFallbacks[ErrTranslationDuplicate]), translationWriteRejectStatus)
}

// ErrNotTranslatableRejected 组装「这段文字本来就不参与翻译」的业务错误（409）。
//
// 复用既有 key（ErrNotTranslatable）而不是再造一条：读侧工作台已经用它做行级说明，
// 写侧拒绝同一条事实时用同一条词条，前端拿到的 reasonKey 才能与行内提示对上。
func ErrNotTranslatableRejected() error {
	return facing.WithStatus(facing.New(ErrNotTranslatable, errFallbacks[ErrNotTranslatable]), translationWriteRejectStatus)
}

// ErrTranslationWriteFailedError 组装「存储层写入失败」的业务错误（500）。
func ErrTranslationWriteFailedError() error {
	return facing.WithStatus(facing.New(ErrTranslationWriteFailed, errFallbacks[ErrTranslationWriteFailed]), translationWriteFailureStatus)
}

// ErrTranslationWriteUnavailableError 组装「写服务未就绪」的业务错误（500）。
func ErrTranslationWriteUnavailableError() error {
	return facing.WithStatus(facing.New(ErrTranslationWriteUnavailable, errFallbacks[ErrTranslationWriteUnavailable]), translationWriteFailureStatus)
}
