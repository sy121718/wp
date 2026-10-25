package maildto

// mail_page_resp.go — 邮件的活动列表页与报表页渲染视图（页面 GET 的回执形状）。
//
// **导出字段名就是模板契约**：internal/templates/admin/mail/*.html 里的 `.Field`。
// 这个模块的几个页面都用 gin.H 拼，缺键的表现是「HTTP 200 + 正文整块消失」
// （Jet 在缺键那一行中断）—— 而编译器对 map 键一无所知。
//
// 零值可用是硬要求：失败档要渲染**同一张模板**（提示条 + 空列表），
// 所以每个字段都必须有可渲染的零值（切片给空切片而不是 nil）。

import "go_wp/pkg/pagination"

// CampaignsPageResp 邮件活动列表整页。
type CampaignsPageResp struct {
	Title         string
	Campaigns     []any
	CampaignTotal int64
	Accounts      []any
	Templates     []any
	Page          int
	// ListQuery 批量删除表单 action 的 query（回跳时由 shell.BackPath 读回，
	// 批量删完不会被弹回第 1 页）。
	ListQuery string
	// Err 取数失败时的归口提示（空串 = 无错误）。
	Err string
	// Pagination 分页事实（模板 admin/partials/pagination.html 按这个键读）。
	Pagination pagination.Page
}

// NewCampaignsPageError 失败档：同一张模板 + 零值 + 提示条。
//
// 空值不是可选的：模板在提示条之后就用 .Page / .CampaignTotal / len(.Campaigns)
// 渲染列表，缺键会让 Jet **在那一行中断**。
func NewCampaignsPageError(title, errText string) *CampaignsPageResp {
	return &CampaignsPageResp{
		Title:     title,
		Campaigns: []any{},
		Accounts:  []any{},
		Templates: []any{},
		Err:       errText,
	}
}

// AutomationRowResp 自动化列表的一行。
//
// 触发方式给**中文标签**（枚举不直接进界面），其余字段原样透出。
type AutomationRowResp struct {
	ID           uint64
	Name         string
	Description  string
	Status       string
	Version      int
	TriggerLabel string
}

// AutomationListPageResp 自动化列表整页（/admin/mail/automation）。
type AutomationListPageResp struct {
	Title     string
	Page      int
	ListQuery string
	// Err 取数失败时的归口提示（空串 = 无错误）。
	Err string
	// Automations / AutoTotal 空列表与 0 不可省：模板随后就用 len(.Automations) /
	// .AutoTotal 渲染列表，缺键会让 Jet 在那一行中断（HTTP 仍是 200、正文整块消失）。
	Automations []AutomationRowResp
	AutoTotal   int64
	// BackPath 列表筛选上下文（回跳时 shell.BackPath 读回）。
	// Buttons 不在这里：它由 adminui.Prepare 从请求上下文注入（模板侧透明）。
	BackPath string

	Pagination pagination.Page
}

// AutomationStepRowResp 自动化编辑页的步骤行。
type AutomationStepRowResp struct {
	Index int
	Key   string
	Type  string
	// 类型下拉与目标下拉（选中态按行数据判）。
	TypeOptions      []OptionResp
	Param            string
	Tag              string
	UnitValue        string
	UnitOptions      []OptionResp
	ConditionOptions []OptionResp
	Yes              string
	No               string
	YesOptions       []OptionResp
	NoOptions        []OptionResp
	StepLabel        string
	NextLabel        string

	IsDelay  bool
	IsEmail  bool
	IsBranch bool
	IsTag    bool
	IsEnd    bool
	IsBlank  bool

	CanMoveUp   bool
	CanMoveDown bool
}

// AutomationFallbackRowResp 表单表达不了的流程的只读行。
//
// 只给**事实**（节点键、类型名、三条跳转），不给可提交的表单：
// 还原失败的表单渲染出来是空的，用户一保存就把还原不出来的节点整段删掉。
type AutomationFallbackRowResp struct {
	Key  string
	Type string
	Next string
	Yes  string
	No   string
}

// AutomationEditPageResp 自动化编辑页（新建 / 编辑 / 只读兜底三态共用一张模板）。
type AutomationEditPageResp struct {
	Title string
	Form  any
	IsNew bool

	// Rows 可编辑的步骤行（只读兜底态为空）。
	Rows []AutomationStepRowResp
	// Fallback / FallbackRows 表单表达不了的流程：只读展示原图 + 去画布的入口。
	Fallback     bool
	FallbackRows []AutomationFallbackRowResp

	Types      []any
	Triggers   []OptionResp
	CanAddStep bool
	// Err 本页的表单校验文案（空串 = 无错误）；写动作结论走提示页。
	Err       string
	Templates []any
}

// AutomationCanvasPageResp 自动化画布页（拖拽编辑器）。
type AutomationCanvasPageResp struct {
	// Title 与小写壳键双出：模板 title 槽是 `{{.title}}`（layout 壳），
	// 而画布页自己还要 `.jsVer`。`view` 标签让同一字段同时供两个键 ——
	// 比在控制器里再拼一层 map 干净（那是这次要消灭的形态）。
	Title        string `view:"title"`
	TitleDisplay string `view:"Title"`
	// A 自动化实体（画布只读元数据 + 节点图）。
	A         any
	NodesJSON string
	MetaJSON  string
	Templates []any
	// 画布顶栏的两个输入控件选项：触发方式下拉与节点类型 palette。
	// 由 enums 与 palette 现算（每次一样、与实体无关），但模板要 range 它们，
	// 所以必须进视图 —— 视图是模板唯一的数据来源。
	Triggers  []OptionResp
	NodeTypes []map[string]any
	// 新建 vs 编辑：新建时名称与触发方式是空/默认值，模板据此给输入框初值。
	IsNew bool
	// 频率两态与「回信即退出」：画布顶栏的单选/复选初值。
	FrequencyEveryTime bool
	FrequencyOnce      bool
	ExitOnReply        bool
	// 邮件节点是否可用：站点没配 marketing 用途的发信账号时灰掉并给原因。
	NodeDisabled       bool
	NodeDisabledReason string
	JSVer     string `view:"jsVer"`
	// KeyframesCSS 按需内联的动效关键帧（本次只用到入场一个）。
	// 不声明就是零字节 —— 后台不常驻加载 63 条营销动效，谁用谁声明。
	KeyframesCSS string
}

// AutomationRunPageResp 自动化运行详情页。
//
// Detail 里 Explain / ErrorMessage / 时间线都是写侧落下的**运行文案编码**，
// 出口按语言还原（见 mailRunDetailTexts）。
type AutomationRunPageResp struct {
	Title string
	D     any
}

// 四个列表页的视图。
//
// 它们的形状高度相似（标题 + 行 + 总数 + 分页），但仍然分开声明：
// 每页的行类型与筛选维度不同，合成一个会让「改这页」牵动那页 ——
// 而这四个页面正是各自独立演进的（联系人加过分组、模板加过 kind）。
// 共用的只有「失败档也要有零值」这一条硬要求。

// AutomationRunsPageResp 自动化运行记录页。
//
// 五个计数键（CountRunning/Waiting/Completed/Failed/Stopped）不可省：模板随后就用它们
// 渲染，缺任一项都会让 Jet 在那一行中断（HTTP 仍是 200、正文整块消失）。
type AutomationRunsPageResp struct {
	Title     string
	Page      int
	FilterID  string
	FilterRun string
	// RunsQuery 补投表单 action 的 query：回跳时由 shell.BackPath 读回，
	// 翻到第 2 页补投后不会被弹回未筛选的第 1 页。
	RunsQuery string
	Err       string

	Runs           []any
	RunTotal       int64
	CountRunning   int64
	CountWaiting   int64
	CountCompleted int64
	CountFailed    int64
	CountStopped   int64

	Pagination pagination.Page
}

// ContactsPageResp 联系人列表页。
type ContactsPageResp struct {
	Title        string
	Contacts     []any
	ContactTotal int64
	Page         int
	// 筛选回显与候选（模板在提示条之后就用它们渲染筛选栏与列表）。
	Keyword    string
	Status     string
	Tags       string
	TagOptions []any
	// ListQuery 写动作表单 action 的 query（筛选 + 页码）。
	ListQuery string
	Err       string

	Pagination pagination.Page
}

// AccountsPageResp 发信账号列表页（/admin/mail）。
//
// 空列表不可省：模板在提示条之后就用 len(.Accounts) / range 取列表，
// 缺键会让 Jet 在那一行中断（HTTP 仍是 200、正文整块消失）。
type AccountsPageResp struct {
	Title    string
	Accounts []any
	Err      string

	Pagination pagination.Page
}

// TemplatesPageResp 邮件模板列表页。
type TemplatesPageResp struct {
	Title         string
	Templates     []any
	TemplateTotal int64
	Err           string
	Pagination    pagination.Page
}

// CampaignReportPageResp 活动报表页。
type CampaignReportPageResp struct {
	Title string
	// R 报表数据（活动信息 + 收件人明细）。
	R          any
	Page       int
	Pagination pagination.Page
}

// TemplateEditorPageResp 模板可视化编辑器页（四栏）。
//
// 与小写壳键双出：模板 title 槽是 `{{.title}}`（layout 壳），编辑器自己还要 `{{.jsVer}}`。
// `view` 标签让同一字段供两个键 —— 比在控制器里再拼一层 gin.H 干净。
type TemplateEditorPageResp struct {
	Title    string `view:"title"`
	T        any
	MetaJSON string
	JSVer    string `view:"jsVer"`
}
