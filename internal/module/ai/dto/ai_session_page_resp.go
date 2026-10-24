package aidto

// PresetOption 内置预设暴露给界面的元信息（**不含任何密钥**）。
//
// 与 service.BuiltinPresetOption 同形：镜像而不是 import，因为 dto 不依赖 service。
// 两者字段不一致时是编译错误的近邻 —— 装配处逐字段拷贝（见 toPresetOptions）。
type PresetOption struct {
	Key         string
	DisplayName string
	BaseURL     string
	Protocol    string
	ModelCount  int
}

// 会话列表页（/admin/ai/sessions）的强类型视图。
//
// 这一页与其它页不同的地方：它**不是一次装配完**的。整页分成五个独立降级的区块
// （列表 / 用量看板 / 供应商卡片 / 详情抽屉 / 事件与折叠预览），各区块失败只影响自己，
// 由 attach* 系列函数依次填充。所以这里按区块分组写字段，而不是摊成一张大表：
// 读代码时能看出「这几项是同一个失败档的」。
//
// 键名与 admin/ai/sessions.html 的点号取值一一对应。
type SessionListPageResp struct {
	// Title 是**已取好词的文本**（layout 壳读 .title，槽位契约是字符串）。
	Title string `view:"title"`

	// 三个壳键：本页不用，但供应商卡片是 include 进来的、
	// 作用域是卡片自己的视图 —— cardData 从这里借（见 ProviderCardBase 的说明）。
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

	// IsModels 是给模板用的布尔：模板里比较字符串（.Tab == "models"）要依赖 Jet 的
	// 比较表达式求值，多一处能出错的地方。
	IsModels bool

	// 标签按权限裁剪：没权限的标签连按钮都不渲染（模板据此包 {{if}}）。
	// 权限是事实：一次取数失败不改变「这个人能看什么」，失败分支照给。
	CanViewModels   bool
	CanViewSessions bool

	// —— 列表 ——

	// LoadFailed / LoadError 会话清单没读出来（这一页的**主数据**）。
	LoadFailed bool
	LoadError  string
	// Rows 每项是 SessionRowResp：行要带上它自己的按 (供应商, 模型) 拆分与最近调用预览。
	Rows  []SessionRowResp
	Total int64

	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevURL    string
	NextURL    string

	// —— 用量看板（独立降级）——

	Usage         SessionUsage
	UsageErr      string
	Trend         SessionTrend
	OptionsErr    string
	FilterOptions SessionFilterOptions

	// —— 供应商卡片（独立降级）——

	// Presets 内置预设清单。用局部镜像而不是 import service 的类型：
	// dto 不能依赖 service（分层方向），而这里只有五个标量字段。
	Presets         []PresetOption
	BuiltinKeys     []string
	ProtocolOptions any
	// ProviderCards 每项是**已摊平的 map**（见 attachProviders 的说明：
	// 卡片模板直接对元素取键，不经过 ViewMap）。
	ProviderCards []any
	Providers     []Provider
	ProviderErr   string

	// —— 详情抽屉（独立降级；只在该区块被装配时才有值）——

	Detail      *SessionDetail
	DetailID    int64
	DetailErr   string
	Events      []SessionEventRowResp
	EventsErr   string
	FoldPlan    *FoldPlanResult
	FoldPlanErr string
}

// SessionRowResp 会话列表的一行。
//
// 三个字段都用结构体/切片而不是 map：模板对行做的是点号取值
// （row.Session / row.Usage / row.Calls），原先那层 gin.H 只是让
// 「字段名拼错」从编译错误退化成渲染期缺键。
type SessionRowResp struct {
	Session Session
	// Usage 这一行的按 (供应商, 模型) 拆分。零值给空切片而不是 nil：
	// 模板写 len(row.Usage)，nil 切片在 Jet 里不是 0 而是运行期错误。
	Usage []SessionModelUsage
	// Calls 最近几次调用的预览（耗时 / token / 成没成）；零值时模板不渲染这块。
	Calls SessionCalls
}
