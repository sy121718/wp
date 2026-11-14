package mailhttp

import (
	"go_wp/pkg/facing"
	"go_wp/pkg/pagination"
)

type CampaignsPageResp struct {
	Title         string
	Campaigns     []any
	CampaignTotal int64
	Accounts      []any
	Templates     []any
	Page          int

	ListQuery string

	Err facing.Msg

	Pagination pagination.Page
}

func NewCampaignsPageError(title string, errFact facing.Msg) *CampaignsPageResp {
	return &CampaignsPageResp{
		Title:     title,
		Campaigns: []any{},
		Accounts:  []any{},
		Templates: []any{},
		Err:       errFact,
	}
}

type AutomationRowResp struct {
	ID          uint64
	Name        string
	Description string
	Status      string
	Version     int
	TriggerType string
}

type AutomationListPageResp struct {
	Title     string
	Page      int
	ListQuery string

	Err facing.Msg

	Automations []AutomationRowResp
	AutoTotal   int64

	BackPath string

	Pagination pagination.Page
}

type AutomationStepRowResp struct {
	Index int
	Key   string
	Type  string

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

type AutomationFallbackRowResp struct {
	Key  string
	Type string
	Next string
	Yes  string
	No   string
}

type AutomationEditPageResp struct {
	Title string
	Form  any
	IsNew bool

	Rows []AutomationStepRowResp

	Fallback     bool
	FallbackRows []AutomationFallbackRowResp

	Types      []any
	Triggers   []OptionResp
	CanAddStep bool

	Err       facing.Msg
	Templates []any
}

type AutomationCanvasPageResp struct {
	Title        string `view:"title"`
	TitleDisplay string `view:"Title"`

	A         any
	NodesJSON string
	MetaJSON  string
	Templates []any

	Triggers  []OptionResp
	NodeTypes []map[string]any

	IsNew bool

	FrequencyEveryTime bool
	FrequencyOnce      bool
	ExitOnReply        bool

	NodeDisabled       bool
	NodeDisabledReason string
	JSVer              string `view:"jsVer"`

	KeyframesCSS string
}

type AutomationRunPageResp struct {
	Title string
	D     any
}

type AutomationRunsPageResp struct {
	Title     string
	Page      int
	FilterID  string
	FilterRun string

	RunsQuery string
	Err       facing.Msg

	Runs           []any
	RunTotal       int64
	CountRunning   int64
	CountWaiting   int64
	CountCompleted int64
	CountFailed    int64
	CountStopped   int64

	Pagination pagination.Page
}

type ContactsPageResp struct {
	Title        string
	Contacts     []any
	ContactTotal int64
	Page         int

	Keyword    string
	Status     string
	Tags       string
	TagOptions []any

	ListQuery string
	Err       facing.Msg

	Pagination pagination.Page
}

type AccountsPageResp struct {
	Title    string
	Accounts []any
	Err      facing.Msg

	Pagination pagination.Page
}

type TemplatesPageResp struct {
	Title         string
	Templates     []any
	TemplateTotal int64
	Err           facing.Msg

	Pagination pagination.Page
}

type CampaignReportPageResp struct {
	Title string

	R    any
	Page int

	Pagination pagination.Page
}

type TemplateEditorPageResp struct {
	Title    string `view:"title"`
	T        any
	MetaJSON string
	JSVer    string `view:"jsVer"`
}
