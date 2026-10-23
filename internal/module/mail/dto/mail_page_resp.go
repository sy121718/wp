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
