package aidto

type PresetOption struct {
	Key         string
	DisplayName string
	BaseURL     string
	Protocol    string
	ModelCount  int
}

type SessionListPageResp struct {
	Title string `view:"title"`

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

	LoadFailed bool
	LoadError  string

	Rows  []SessionRowResp
	Total int64

	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string

	Usage         SessionUsage
	UsageErr      string
	Trend         SessionTrend
	OptionsErr    string
	FilterOptions SessionFilterOptions

	Presets         []PresetOption
	BuiltinKeys     []string
	ProtocolOptions any

	ProviderCards []any
	Providers     []Provider
	ProviderErr   string

	Detail      *SessionDetail
	DetailID    int64
	DetailErr   string
	Events      []SessionEventRowResp
	EventsErr   string
	FoldPlan    *FoldPlanResult
	FoldPlanErr string
}

type SessionRowResp struct {
	Session Session

	Usage []SessionModelUsage

	Calls SessionCalls
}
