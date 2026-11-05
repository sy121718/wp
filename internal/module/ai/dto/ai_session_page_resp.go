package aidto

import "go_wp/pkg/pagination"

type PresetOption struct {
	Key         string
	DisplayName string
	BaseURL     string
	Protocol    string
	ModelCount  int
}

// SessionListPageResp 是「大模型管理」整页的数据。
//
// 没有 Title：页面标题是界面文案，归模板的 pageTitle/topbarTitle block 用全局 t 取词。
// 布局的 <title> 兜底会直接渲染 .title，把 key 塞进来就是裸 key 上屏。
type SessionListPageResp struct {
	CSRFToken string `view:"csrf_token"`
	T         any    `view:"t"`
	PermSet   any    `view:"PermSet"`
	Buttons   any    `view:"Buttons"`

	Page     int
	PageSize int
	Filter   SessionQuery
	Keyword  string
	Status   int
	Tab      string

	IsModels bool

	CanViewModels   bool
	CanViewSessions bool

	Rows  []SessionRowResp
	Total int64

	// Pagination 是分页事实（pkg/pagination.Page）：按钮窗口与每页链接由模板调用
	// 共享分页能力算出，后端只交总数、当前页与每页条数。
	Pagination pagination.Page

	Usage         SessionUsage
	Trend         SessionTrend
	FilterOptions SessionFilterOptions

	Presets         []PresetOption
	BuiltinKeys     []string
	ProtocolOptions any

	ProviderCards []any
	Providers     []Provider

	Detail   *SessionDetail
	DetailID int64
	Events   []SessionEventRowResp
}

type SessionRowResp struct {
	Session Session

	Usage []SessionModelUsage

	Calls SessionCalls
}
