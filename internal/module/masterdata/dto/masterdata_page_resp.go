package masterdatadto

import (
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/pagination"
)

// masterdata_page_resp.go — 变更记录页的整页视图（页面 GET 的回执形状）。
//
// 与 masterdata_resp.go 的分工：那一份是**接口契约**（JSON API 的 ChangeResp /
// EntityHistoryResp，逐字段原始值）；这一份是**页面视图**（已取好词、已算好展示文本，
// 模板直接读）。分开的理由与 product 的两份一样：读一个类型不必先判断它是进还是出。
//
// 为什么从 gin.H 换成强类型（AGENTS.md 分层表；商品模块已走完这条路）：
//
//  1. **模板契约可对账**。字段名就是模板里的 `.Field`；gin.H 拼错键名 → 渲染期拿不到值
//     → 显示空白，编译器一句话也不说。强类型被改名 = 编译错误，模板侧由反射对账测试钉住。
//  2. **页面状态有了归属**。失败态用 PageStatus() 表达（零值 = 200 可缓存），
//     与 pkg/view.Result 同一口径；gin.H 时代失败态只能靠 handler 临时拼。
//  3. **分页仍是现成对象**。adminui.AttachPagination 往「现成数据对象」上挂一组
//     小写壳键（total/page/limit/base/...），模板侧与 gin.H 时代完全一样 ——
//     换 DTO 不改变这条既有的分工。
//
// **导出字段名就是模板契约**：internal/templates/admin/masterdata/ 下 .jet 里的
// `.Field` 必须在这里有同名导出字段。改动字段名要同步改模板与对账测试。
// （下面每个字段的注释写明它对应模板里哪一处判断，便于改动时评估影响面。）

// ChangeRowResp 变更记录表的一行（已取词、已算展示文本）。
type ChangeRowResp struct {
	Time            string // 展示用时间（本地时区，分钟精度；空值显示「—」）
	EntityType      string // 原始实体类型（筛选用）
	EntityTypeLabel string // 当前语言的实体类型名
	EntityID        string
	EntityLabel     string
	Action          string // 原始动作（筛选用）
	ActionLabel     string // 当前语言的动作名
	Field           string // 原始字段名（筛选用）
	FieldLabel      string // 当前语言的字段名
	OldValue        string // 已格式化（空值 → 「—」）
	NewValue        string
	Origin          string // 变更来源（页面 / API / 导入…）
	OperatorID      string // 已格式化
}

// EntityRowResp 实体聚合表的一行（「哪个实体改得最多」的入口）。
type EntityRowResp struct {
	EntityType      string
	EntityTypeLabel string
	EntityID        string
	EntityLabel     string
	ChangeCount     int64
	LastAction      string
	LastActionLabel string
	LastField       string
	LastFieldLabel  string
	LastOperatorID  string
	LastAt          string
	// URL 锁定该实体的筛选链接（点「查看历史」切回记录视图并带上该实体）。
	URL string
}

// EntityTimelineHeadResp 锁定单实体时的时间线标题信息（模板读 .Current.*）。
//
// 指针语义：nil = 未锁定（按全部记录展示），非 nil = 当前锁定了这个实体。
// 模板用 `{{ if .Current }}` 判断，因此这里必须是指针而不是零值结构体 ——
// 零值结构体在 Jet 里是真值，会把「未锁定」渲染成一块空白标题。
type EntityTimelineHeadResp struct {
	EntityType      string
	EntityTypeLabel string
	EntityID        string
	EntityLabel     string
	Total           int64
}

// LabelOptionResp 下拉选项（实体类型 / 动作）。与 product 的 *OptionResp 同形。
type LabelOptionResp struct {
	Value string
	Label string // 已取词
}

// MasterDataChangesPageResp 变更记录整页的渲染视图。
type MasterDataChangesPageResp struct {
	// Title / Menu 是 layout 的小写壳键（{{.title}} / {{.menu}}）的值；
	// Title 放 enums 的 i18n key，翻译由 adminui.Prepare 做。
	Title string
	Menu  string

	// Projects / SelectedProject 工程上下文（失败页也带着它：
	// 页壳与返回链接不因取数失败缺一块）。
	//
	// 元素类型用工程契约的 ProjectResp：模板只读 .Name / .ID 这类既有字段，
	// 用 []any 会让模板侧的类型信息彻底消失（也就无法与契约对账）。
	Projects        []projectdto.ProjectResp
	SelectedProject string

	// EntityTypes / Actions 筛选项的选项表（白名单来自模块 enums）。
	EntityTypes []LabelOptionResp
	Actions     []LabelOptionResp

	// 当前筛选条件逐项传模板（Jet 对「map 再索引」的链式访问不确定）。
	FilterEntityType string
	FilterEntityID   string
	FilterField      string
	FilterAction     string
	FilterKeyword    string
	FilterOperatorID string
	FilterSince      string
	FilterUntil      string
	FilterSpecific   bool

	// ViewMode 视图切换：records（逐条流水，默认） / entities（按实体聚合）。
	ViewMode string
	// RecordsViewURL / EntitiesViewURL 两个切换链接（同一份筛选条件，切视图不丢筛选）。
	RecordsViewURL  string
	EntitiesViewURL string

	// Current 锁定单实体时的时间线标题（nil = 未锁定）。
	Current *EntityTimelineHeadResp

	Rows  []ChangeRowResp
	Total int64

	Entities    []EntityRowResp
	EntityTotal int64
	// EntityShown 当前展示条数；EntityPageSize 每页上限。
	EntityShown    int
	EntityPageSize int
	// EntityTruncated 实体清单被截断（EntityTotal > EntityShown）时给一句提示。
	// 比较运算留在 handler，模板只做判断。
	EntityTruncated bool

	// Err 错误提示（本次查询错误优先于 ?err= 回显），空串表示无错误。
	Err string
	// LoadFailed：本次请求的工程列表没读到（**不是**「没有数据」）。
	// 模板据此把两个面板的空态换成「没读出来」—— 空态标题若是「没有记录」，
	// 用户会以为历史真的没了，而实际上只是这一次没读出来。
	LoadFailed bool

	// Pagination 分页事实（由 adminui.AttachPaginationView 反射挂上）。
	//
	// 键名必须是 view.PaginationKey（"Pagination"）—— 模板的
	// admin/partials/pagination.html 按这个键读。字段显式声明在这里，
	// 而不是让渲染层"偷偷长一个出来"：模板契约要看得见。
	Pagination pagination.Page

}

// 失败态怎么表达（与商品模块同一形态，两类分开）：
//
//  1. **页内降级** —— 某一块取数失败但页面整体还能读（本页的工程清单读不到、
//     某张表查失败）：留在**同一个视图**上，用 LoadFailed / Err 表达，仍是 200。
//     这是本页唯一的降级形态：两张表各自失败都只影响自己，页壳与筛选栏始终在。
//  2. **整页失败** —— 页面压根撑不起来（视图构建失败等）：走 pageview.Fail(c, err)，
//     由 pkg/response 按错误性质给状态码与统一文案（JSON 出口）。
//
// 不实现 view.Result 的 PageStatus/PageNoStore：那两个方法会被
// internal/architecture 的「dto 不带展示字段」门禁计入（它把带接收者的
// PageStatus/PageNoStore 视为展示语义落到了 dto 上），而本页没有「失败页视图」
// 这种第二形态 —— 硬加只会为了凑接口造出一个多余的失败视图。
