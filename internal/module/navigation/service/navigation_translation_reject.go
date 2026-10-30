package navigationservice

// navigation_translation_reject.go — 译文整表拒绝的**可定位明细**（facing.BlockedError 的实现）。
//
// 为什么单独一个文件：拒绝形状是写服务的对外契约（出口用 errors.As 认 facing.BlockedError，
// 逐条翻 reasonKey 放进 data.items），而写服务本体的注释已经讲的是流转顺序 ——
// 把「拒绝长什么样」与「怎么走到拒绝」分开，读的人各看各的。
//
// 层归属：service —— 它把业务判定结出的**事实**（哪一行、服务端原文是什么、为什么）
// 拼成错误形状。不取词：reasonKey 与兜底都由 enums 声明处给，出口只调一次取词。

import (
	"go_wp/pkg/facing"
)

// TranslationRejectedError 整批拒绝：一句话说结论，可定位明细放 data.items。
//
// 包装而非另造一套：Err 是 facing 的常规业务错误（key + 兜底 + 声明处给的状态码），
// Unwrap 让 response.ErrorStatusAuto / facing.FactOf / StatusOf 沿链读到它们 ——
// 出口不需要认识本类型，照常走同一套判据。
type TranslationRejectedError struct {
	Err error
	// Items 被拒的行（id = 原文 hash、name = 服务端原文 / 未知时回落 hash、
	// reasonKey = 声明处给的错误 key）。
	Items []facing.BlockedItem
}

// Error 返回包装错误的语义（整条消息 = i18n key，与 facing.Error 的形态一致）。
func (e *TranslationRejectedError) Error() string { return e.Err.Error() }

// Unwrap 让 errors.Is / errors.As / StatusOf 读到声明处的那条业务错误。
func (e *TranslationRejectedError) Unwrap() error { return e.Err }

// BlockedItems 让出口按接口认形状（pkg/response 只认 facing.BlockedError）。
func (e *TranslationRejectedError) BlockedItems() []facing.BlockedItem { return e.Items }

// 编译期断言：出口的判据认这个形状。
var _ facing.BlockedError = (*TranslationRejectedError)(nil)
