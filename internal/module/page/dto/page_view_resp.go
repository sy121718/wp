package pagedto

import (
	"go_wp/pkg/facing"
	"go_wp/pkg/utils"
)

// PageLangResp 面板里的一行语言（状态文案已按当前语言取词）。
type PageLangResp struct {
	Lang string
	// IsDefault 站点默认语言（不可排除）。
	IsDefault bool
	// Excluded 本页已排除该语言。
	Excluded bool
	// Published 该语言当前有已激活产物。
	Published bool
	// StatusKey / StatusFallback 状态文案的**事实**（已排除 / 已发布 / 未发布）。
	//
	// 行上放事实而不是成品串：语言是请求级的，控制器提前翻等于在这里替渲染期做决定；
	// 片段模板拿到 adminui.PrepareBare 注入的 tr，按当前请求语言取词。
	StatusKey      string
	StatusFallback string
	// CanRepublish 可以点「重新发布」（未排除即可；已排除的要先恢复）。
	CanRepublish bool
	// NoteKey / NoteFallback 不可操作的原因（默认语言）的事实；两个都空 = 可操作。
	NoteKey      string
	NoteFallback string
}

// ScheduleResp 只交动作与状态事实；Note 是受控业务错误译文。
type ScheduleResp struct {
	ID     int64
	Action string
	Lang   string
	// ScheduledAt 到点时刻（事实）：格式由模板的 dateTime() 决定。
	ScheduledAt utils.JSONTime
	Status      string
	Attempts    int
	// NoteKey / NoteFallback 失败原因的**事实**（写侧只写 key，读侧按 key 取词）。
	//
	// 为什么行上不放成品串：与 Note 一起交出去的还有「用哪种语言说」这个决定，
	// 而语言是请求级的 —— 取词放在模板（片段拿到 adminui.PrepareBare 注入的 tr）。
	NoteKey      string
	NoteFallback string
	// CanCancel 只有 pending 能取消（running 已在执行、终态没有可取消的东西）。
	CanCancel bool
}

// PageRowResp 列表行投影：时间交事实，UI 文案由 Jet 选择。
type PageRowResp struct {
	ID        string
	ProjectID string
	Kind      string
	DraftPath string
	Active    bool
	Staged    bool
	Stale     bool
	Version   int64
	UpdatedAt utils.JSONTime

	// 定时上下线（PIPE-7）的行内投影：有待执行排定时显示「已排定 + 到点时刻」，
	// 有失败排定时显示「排定失败 + 原因」。
	//
	// 取词仍在 Go 侧（失败原因是受控错误枚举 → 当前语言文案），**时间交事实**：
	// SchedulePendingAt 是时间对象，站点时区与格式由模板的 siteDateTime 决定
	//（task-108 迁移；档位同理由模板按事实选，模板是磁盘热读文件，判据写进模板会漂移）。
	SchedulePendingAt *utils.JSONTime
	// ScheduleFailedKey / ScheduleFailedFallback 排定失败原因的**事实**（行上不放成品串：
	// 徽标的 title 由模板按当前请求语言取词）。
	ScheduleFailedKey      string
	ScheduleFailedFallback string
	// ScheduleAlert 是否有需要运营看一眼的失败排定（模板据此选徽标样式）。
	ScheduleAlert bool
}

// HistoryRowResp 修订历史行视图。
//
// CreatedAt 交的是**事实**（时间对象），展示格式由模板的 dateTime() 决定 —— 与 PageRowResp 同一口径：
// Go 只交时刻，不替模板选格式。
type HistoryRowResp struct {
	Version   int64
	Path      string
	CreatedAt utils.JSONTime
}

// StaleItemResp 「全站待重建」清单的一行。
//
// 跨工程清单必须带工程名：每个工程都可能有一个一模一样的 /about，只给路径分不清是哪一个
// （页面列表页本身有「所属工程」上下文，这一块没有）。
type StaleItemResp struct {
	ID          string
	Path        string
	ProjectName string
	// Published 是否已上线（有活跃产物路径）：用来区分「已发布但有更新未发布」与「从未上线」——
	// 后者的处置方式不同（重建也还不会出现在访问面，要先发布）。
	Published bool
	// FailedStageKey / FailedStageFallback 最近一次自动重建失败的**阶段事实**
	// （两个都空 = 没有失败痕迹）。非空即「这页不是还没轮到，而是重建失败过」——
	// 与 stale 徽标合起来才能回答「为什么它还在这儿」。
	//
	// 行上不放拼好的句子：那一句由「前缀词条 + 阶段词条 + 时刻」三段组成，
	// 控制器拼的那一刻就是它替渲染期定语言的时刻（语言是请求级的）。
	FailedStageKey      string
	FailedStageFallback string
	// FailedAt 失败时刻（事实；格式由模板的 siteDateTime 决定）。
	FailedAt *utils.JSONTime
}

// TranslationLangOptionResp 工作台语言下拉项。
type TranslationLangOptionResp struct {
	Code   string
	Label  string
	Active bool
}

// TranslationRowResp 工作台一行（一个可翻译取值）。
type TranslationRowResp struct {
	Context    string
	Component  string
	Field      string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	Rich       bool
	Limit      int
	// ReusePages 除本页外还用到该 (原文, 语境) 的页面数（0 = 仅本页）。
	ReusePages int
	// ReuseTotal 该 (原文, 语境) 在全站出现的页面总数。
	ReuseTotal int
	// ReusePaths 复用路径事实；截断与分隔文案由 Jet 决定。
	ReusePaths []string
	// Origin 来源事实（空 = 本页；header / footer / block = 共享块）。
	Origin string
}

// TranslationGroupResp 按组件分组的行集合。
type TranslationGroupResp struct {
	Component string
	Rows      []TranslationRowResp
}

// TranslationRowErrorResp 逐行错误：Label 是「哪一行」（原文 context 名，可能为空 = 页面级），
// Msg 是结论事实。
//
// 不把 Label 拼进 Msg.Fallback：拼了就成成品串，模板再取词也救不回来（中文永远粘在英文词条前面）。
// 也不把 Label 塞进 Msg.Args —— 那个位置留给词条自身的 `%s` 参数。
type TranslationRowErrorResp struct {
	Label string
	Msg   facing.Msg
}
