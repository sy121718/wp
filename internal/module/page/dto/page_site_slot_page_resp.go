package pagedto

// page_site_slot_page_resp.go — 系统页面槽位页的渲染视图（页面 GET 的回执形状）。
//
// **导出字段名就是模板契约**：internal/templates/admin/page/site_slots.html 里的
// `.Field` 必须在这里有同名导出字段。
//
// 这一页有一个反复踩过的坑写在下面：页头两个徽标的档位**不由 Go 产出**，模板按计数
// 自己选档（判据 `n != 0`，含负数非零）。视图只交计数事实。

// SiteSlotPageOptionResp 绑定表单里的页面选项（模板读 .ID / .Label）。
type SiteSlotPageOptionResp struct {
	ID       string
	Label    string
	Selected bool
}

// SiteSlotRowResp 槽位表的一行。
//
// 槽位名与用途交的是**词条 key + 中文兜底**（pageenums 的两张表就是词条键），
// 取词交给模板 —— 只有模板才知道当前请求的语言，在 Go 里预先翻等于替渲染期做决定
// （英文后台下会显示中文，且不报错、没日志）。
type SiteSlotRowResp struct {
	Slot string

	SlotNameKey      string
	SlotNameFallback string
	UsageKey         string
	UsageFallback    string

	Bound       bool
	Unbound     bool
	PageDeleted bool
	PageID      string
	DraftPath   string
	Path        string
	PublicURL   string
	Published   bool
	// Unpublished 单独给一个布尔：它就是「运营以为配好了、实际链接生成方会跳过」的那一类。
	Unpublished bool

	PageOptions []SiteSlotPageOptionResp

	// Active 绑定面板正开着的是这一行（行内链接据此标 aria-current，
	// 读屏用户能听出「面板正开着的是这一行」）。
	Active bool
}

// SiteSlotsPageResp 系统页面槽位整页的渲染视图。
type SiteSlotsPageResp struct {
	Title           string
	Menu            string
	Projects        []any
	SelectedProject string

	Rows []SiteSlotRowResp
	// HasPages 工程里有页面（否则绑定表单不渲染空下拉 —— 空下拉只会让运营点一个必然失败的提交）。
	HasPages bool
	NoPages  bool

	BoundCount       int
	Total            int
	UnpublishedCount int
	DeletedCount     int

	// Err 取数失败时的**词条事实**（Key + Fallback 都空 = 无错误）。
	// 模板用 tr(key, fallback) 取词 —— 视图不存成品句。
	Err MsgPair
	// NoProjectEmpty 「还没有站点工程」空态：工程列表为空 **且** 本次没有出错。
	//
	// 不能只判 len(Projects) == 0：装载失败时工程列表同样是空的，但那时该显示错误条，
	// 而不是「先去建一个工程」—— 用户明明有工程，是这一页没读出来。引导他去建一个
	// 已存在的工程，比没有提示更坏。
	NoProjectEmpty bool

	// —— 绑定面板（bindSlot 命中槽位清单时才填充）——
	BindSlot             string
	BindSlotNameKey      string
	BindSlotNameFallback string
	BindBound            bool
	BindDeleted          bool
	BindPageOptions      []SiteSlotPageOptionResp
}

// MsgPair 词条事实（key + 中文兜底）。取词交给模板。
type MsgPair struct {
	Key      string
	Fallback string
	// Args 词条的 %s 实参（空 = 该词条没有占位符）。
	// 模板用 tf(key, fallback, args) 取词 —— 与 adminui.Msg 同形，页壳那条链路认这个形状。
	Args []string
}

// Empty 两项都空 = 没有这条提示。
func (m MsgPair) Empty() bool { return m.Key == "" && m.Fallback == "" }
