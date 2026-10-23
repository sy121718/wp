package aidto

// ai_page_resp.go — AI 后台页的渲染视图（页面 GET 的回执形状）。
//
// **导出字段名就是模板契约**：internal/templates/admin/ai/*.html 里的 `.Field`。
// Jet 对缺键是**整页中断**（HTTP 200 + 正文整块消失），而编译器对 map 键一无所知 ——
// 这就是这一批从 gin.H 换过来的理由。
//
// 小写字段（Title/Menu）是 layout 壳读的键，而 Go 的导出字段必须大写；
// 壳键由 adminui.Prepare 的 ViewMap 用**字段名**摊平，所以模板读 `.Title`。
// 原先 gin.H 里同时有 "title" 与 "Title" 两个键的页面，迁过来之后只剩 `Title`，
// 模板那边已经统一读 `.Title`。

// McpPageResp MCP 与外部访问页。
type McpPageResp struct {
	// Title 页面标题（壳与正文共用一次取词结果 —— 读两次若中间被改，
	// 会出现「文字说已开启、颜色说已关闭」）。
	Title        string
	Lead         string
	Endpoint     string
	Tokens       []any
	Tools        []any
	ScopeOptions []any
	NewToken     string
	ErrText      string
	DoneText     string
	// McpEnabled 开关状态只读一次：页面要如实显示它（运维判断「外面能不能进来」的唯一地方）。
	McpEnabled bool
	// McpTone 开关徽标的档位。与 McpEnabled 同源，避免读两次导致不一致。
	McpTone string
}

// McpTokenRowResp MCP 令牌表的一行。
type McpTokenRowResp struct {
	ID        string
	Name      string
	Scope     string
	CreatedAt any
	LastUsed  any
	Revoked   bool
}

// PickerPageResp 模型选择弹窗所在的整页（非 htmx 请求的退回入口）。
type PickerPageResp struct {
	Title  string
	Picker *PickerResp
}

// PickerResp 模型选择弹窗的数据。
type PickerResp struct {
	Provider   any
	Candidates []string
	Existing   map[string]bool
}

// SessionEventRowResp 会话事件表的一行。
//
// **字段名 = 模板键**（ai_session_detail.html / ui_blocks.html 读 `.Seq` / `.Views` 等）。
type SessionEventRowResp struct {
	Seq       int64
	Kind      string
	Tone      string
	Content   string
	Views     []any
	HasViews  bool
	TimeLabel string
	// Tool / Phase 工具事件把工具名与阶段提出来单独显示：正文里可能只有
	// 「工具执行失败」几个字，是哪个工具、是调用还是结果，只有结构化字段说得清。
	Tool  string
	Phase string
	// ViewsCtx ui_blocks.html 自己 range .Views，所以 include 时得给它一个
	// 带这个键的上下文，而不是直接把切片丢过去（Jet 的 include 只接受变量表达式）。
	ViewsCtx *FabViewsCtx
}

// ModelRowResp 模型目录表的一行（模板读 .Index / .DisplayName / .TextChecked …）。
//
// Index 是**行序号**：模板用它拼表单字段名（models[0].id 这类），不是展示序号。
type ModelRowResp struct {
	Index           int
	ID              string
	DisplayName     string
	ContextWindow   int64
	MaxOutputTokens int64
	TextChecked     bool
	ImageChecked    bool
}

// ModelsDataResp 模型目录区的模板数据（htmx 片段 + 卡片共用）。
type ModelsDataResp struct {
	Provider any
	Rows     []ModelRowResp
	// ModelsNotice 目录读取结果提示（已取好词：这个槽位在模板里直接渲染文本）。
	ModelsNotice    string
	ModelsNoticeErr bool
	// ModelsNoticeTone / ModelsNoticeRole 分档与无障碍角色。
	//
	// provider_models.html 有**两条渲染路径**（整页 include 走 cardData、htmx 片段单独
	// 渲染走这里），只补一处会渲染出 `badge badge-`（空类名）。
	ModelsNoticeTone string
	ModelsNoticeRole string
}

// ProviderCardResp 一个供应商卡片（含模型目录区）的模板数据。
//
// csrf_token / t / PermSet 从页面基座 map 里借 —— 卡片是 include 进整页的，
// 作用域是这张卡片自己的 map，拿不到整页的键。这三个键由调用方显式传入。
type ProviderCardResp struct {
	Provider        any
	Rows            []ModelRowResp
	ProtocolOptions any
	DefaultBaseURL  string
	BuiltinKeys     any
	ModelsNotice    string
	ModelsNoticeErr bool
	// 卡片上两处徽标的分档：启停与「模型目录读取结果」。
	StatusTone       string
	ModelsNoticeTone string
	ModelsNoticeRole string

	CSRFToken string `view:"csrf_token"`
	// T / PermSet 是**从基座借来的壳键**（取值函数与权限集合）。
	T       any `view:"t"`
	PermSet any `view:"PermSet"`
}
