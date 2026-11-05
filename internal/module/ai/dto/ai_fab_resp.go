package aidto

// FabViewsCtx 是 fab 视图块的上下文；/ai/ask/stream 与历史回放都用它。
//
// 另三个类型（FabResultView / FabErrorView / FabMsgFact）随死端点 POST /ai/ask 一起退役：
// 前端只走 /ai/ask/stream（见 internal/templates/static/js/ui/ai-fab.js 的 fetch），
// 那份「返回一段结果片段」的形状没有任何真实消费者。
type FabViewsCtx struct {
	Views []any
}
