package aidto

import "go_wp/pkg/utils"

// McpPageResp 是 MCP 页的整页数据。
//
// 这里没有 Title / Lead：页面标题与导语是界面文案，归模板用全局 t 取词
// （布局的 <title> 兜底会直接渲染 .title，塞进来就是把裸 key 端上屏）。
type McpPageResp struct {
	Endpoint     string
	Tokens       []any
	Tools        []any
	ScopeOptions []any
	McpEnabled   bool
}

// PickerResp 是「获取可用模型」回给前端的事实：提供者（含 version，提交时要带回来）、
// 候选 id 列表、以及已在目录里的 id 集合（前端据此置灰并标「已在目录」）。
//
// JSON 键显式声明：前端按 provider / candidates / existing 取值。
type PickerResp struct {
	Provider   any             `json:"provider"`
	Candidates []string        `json:"candidates"`
	Existing   map[string]bool `json:"existing"`
}

type SessionEventRowResp struct {
	Seq      int64
	Kind     string
	Content  string
	Views    []any
	HasViews bool

	// At 是事件发生的时刻（事实）。显示格式归模板的 dateTime，不在后端拼成品串。
	At utils.JSONTime `json:"at"`

	Tool  string
	Phase string

	ViewsCtx *FabViewsCtx
}

// ModelRowResp 是模型行的事实形状。JSON 键显式声明：前端按这些键重绘行，
// 键名一旦跟着 Go 字段名（大写开头）走，前端取值就全空。
type ModelRowsResp struct {
	Provider Provider       `json:"provider"`
	Rows     []ModelRowResp `json:"rows"`
}

// ModelRowResp 只带模型本身的事实。
//
// 没有 Index / TextChecked / ImageChecked：行序号是「第几条」、勾选是「Type 里有没有
// text/image」—— 两者都是显示派生，模板用 {{range i, row := .Rows}} 与 InputTypes
// 自己算，JS 重建行时同样自己算。后端算好放进来会让同一件事有两个事实源，
// 前端一旦按行重排（删掉中间一行）就与后端算的对不上。
type ModelRowResp struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"displayName"`
	ContextWindow   int64    `json:"contextWindow"`
	MaxOutputTokens int64    `json:"maxOutputTokens"`
	InputTypes      []string `json:"inputTypes"`
}

// ProviderCardBase 是卡片的请求级事实（CSRF、权限集合与按钮）与业务实体的组合输入。
// 取词不走这里：共享渲染已按请求注入全局 t / tf / tn，模板不再从 data 读翻译函数。
type ProviderCardBase struct {
	CSRFToken string
	PermSet   any

	Buttons any
}

type ProviderCardResp struct {
	Provider        any
	Rows            []ModelRowResp
	ProtocolOptions any
	DefaultBaseURL  string
	BuiltinKeys     any

	CSRFToken string `view:"csrf_token"`

	PermSet any `view:"PermSet"`
	// Buttons 是权限按钮集合，模板用 isset(.Buttons["ai.provider_save"]) 判控件可见性。
	// 卡片没透传它，isset 恒为假 —— 所有写控件（保存/启停/删除/模型区）都会被 hidden 掉。
	Buttons any `view:"Buttons"`
}
