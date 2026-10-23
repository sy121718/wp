package aidto

// ai_fab_resp.go — AI 悬浮球结果片段（partials/ai_fab_result）的渲染视图。
//
// 这是**片段出口**：FabAsk 与服务端渲染共用它，失败与成功走同一条出口
// （只换字段，不换模板）。合成一个"成功/失败二选一"的联合类型在这里不划算 ——
// 片段的字段本就稀疏，模板用 `{{if .FabError}}` 分流，缺键即假。
//
// **导出字段名就是模板契约**：internal/templates/partials/ai_fab_result.html

// FabMsgFact 词条事实（key + 中文兜底 + %s 参数），取词交给模板的 tf。
//
// 与 adminui.Msg 同形：那个类型是页壳链路的输入，dto 不该 import internal/adminui。
type FabMsgFact struct {
	Key      string
	Fallback string
	Args     []string
}

// FabResultView 悬浮球消息结果的渲染数据。
type FabResultView struct {
	// FabError 出错时的词条事实（零值 = 没出错）。
	FabError FabMsgFact
	// FabAskKey 本次请求的上下文标识（用于"重试这一条"）。
	FabAskKey string

	// FabAnswer 助手回答正文；FabSessionID 会话 id（后续追问要它）。
	FabAnswer    string
	FabSessionID int64

	// FabReasoning 思考过程（会话层写进助手事件 meta 的那段）。
	FabReasoning string
	// FabTruncatedText 输入被截断时的提示。
	FabTruncatedText string

	// FabViewsCtx 可渲染视图的包装（ui_blocks.html 自己 `{{range .Views}}`，
	// 所以 include 时必须传一个带 Views 键的上下文，不能把切片本身传进去 ——
	// Jet 的 include 只接受变量表达式，不接受 map 字面量）。
	FabViewsCtx *FabViewsCtx

	// 工具调用的计数与前后缀（模板拼「已调用 N 个工具」那句）。
	FabToolsCount  int
	FabToolsPrefix string
	FabToolsSuffix string
	FabThinkLabel  string
	FabEmptyText   string
}

// FabViewsCtx 视图列表的包装（见 FabResultView.FabViewsCtx 的注释）。
type FabViewsCtx struct {
	Views []any
}

// FabErrorView 悬浮球**纯错误**帧的渲染数据（只有一行提示，没有答案）。
type FabErrorView struct {
	FabError  FabMsgFact
	FabAskKey string
}
