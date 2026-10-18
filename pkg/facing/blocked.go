// blocked.go — 整批被拒时「哪几条挡住了」的形状。
//
// 为什么放在 facing 而不是 pkg/response：这是**错误的形状**，不是出口的实现。
// service 声明错误时把明细挂上，出口（JSON / 页面 / AI 工具）各自决定怎么展示 ——
// 出口若自己定义这个类型，第二个出口就得再定义一次，两边字段名一漂移，
// 前端拿到的 items 就少几个键（静默空白，不报错）。
//
// 本包仍然零业务依赖：这里只有字符串字段，不认任何模块的 dto。
package facing

// BlockedItem 整批被拒的一条定位信息。
//
// 三个字段各自解决一件事：ID 用于「重试时勾哪一条」、Name 用于人读（批量里给 uuid
// 等于没给信息）、ReasonKey 用于说明为什么。
//
// Fallback 是从**错误声明处**取来的中文兜底（service 侧调 enums 的兜底表）：出口有语言，
// 但它不该替声明方猜「这个词条缺了该显示什么」。为空时出口按 key 原文显示。
type BlockedItem struct {
	ID        string
	Name      string
	ReasonKey string
	Fallback  string
}

// BlockedError 整批被拒的错误：结论在 Error()，可定位明细在这里。
//
// 出口用 errors.As 认这个形状（不认具体类型）：只要实现了它，就把明细放进响应的
// data.items 并逐条翻译 reasonKey。没有实现它就是一条普通业务错误。
type BlockedError interface {
	error
	BlockedItems() []BlockedItem
}
