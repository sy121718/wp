package pagehttp

// page_view_handle.go — 页面出口的视图类型（模板直接读的导出字段）。
//
// 与 dto 的分工：dto 只放请求绑定与接口响应；这些类型只被 pageview.RenderTemplate /
// adminui.PrepareBareView 消费，导出字段名就是模板契约键。
//
// 原先它们住在 dto 包里，于是「页面视图」与「接口 DTO」在同一个包中混居：
// 想回答「这个类型是给模板的还是给 API 的」只能靠逐个读消费点。
//
// PageLangStateResp 留在 dto：它带 json tag、经 contract 别名对外暴露（接口 DTO）。

import (
	pagedto "go_wp/internal/module/page/dto"
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

// PageRowResp 列表行投影：时间交事实，UI 文案由 Jet 选择。
type PageRowResp struct {
	ID           string
	ProjectID    string
	Kind         string
	DraftPath    string
	ActivePath   string
	Publications []pagedto.PagePublicationResp
	Active       bool
	Staged       bool
	Stale        bool
	Version      int64
	UpdatedAt    utils.JSONTime

	// LangScope 按语言维度筛选时，本行在**那一门语言**下的产出状态（空 = 本次没按语言筛，
	// 行上不该出现语言徽标）。
	//
	// 三个字段分工：Status 是取值（模板据此选徽标档位，档位是展示词表的事），
	// Key / Fallback 是文案事实（取词在模板里，本层不拼成品句）。
	LangScope     string
	LangStatusKey string
	LangStatusFb  string
}

// TranslationLangOptionResp 工作台语言下拉项。
type TranslationLangOptionResp struct {
	Code   string
	Label  string
	Active bool
}

// PageListLangOptionResp 页面列表筛选栏的「语言」下拉项（一门站点启用语言）。
//
// Value 是完整语言码（提交出去的 query 值），Label 是展示文本：两者现在相同，
// 但分开是为了将来接语言显示名时不改提交值 —— 提交值一旦变成显示名，
// 手改 URL 就再也对不上语言清单。
type PageListLangOptionResp struct {
	Value   string
	Label   string
	Default bool
	Active  bool
}

// PageListStateOptionResp 页面列表筛选栏的「状态」下拉项。
//
// Key / Fallback 是状态文案的**事实**（取值同 pageenums.LangStatusOf）：
// 下拉与行徽标必须用同一份词条，否则同一个「已排除」在两处会写成两种说法。
type PageListStateOptionResp struct {
	Value    string
	Key      string
	Fallback string
	Active   bool
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

// TranslationMissPageResp 未翻译清单页（/admin/page-translation-misses）的视图。
//
// 它是**报告片段**：整页由前端壳承载，这个 handler 只出报告区那块。
type TranslationMissPageResp struct {
	CSRFToken       string
	Projects        any
	SelectedProject string
	Rows            any
	// Err 取数失败的归口事实（key + 中文兜底），取词在模板里发生。
	Err any
}

// PageHistoryListResp 版本历史列表片段（fragments/history_list）。
type PageHistoryListResp struct {
	// Revisions 用空切片而非 nil：Jet 的 len() 不接受 nil（会渲染失败）。
	Revisions []HistoryRowResp
	// Error 是**事实**（key + 中文兜底），取词在片段模板里做。
	Error any
}

// TranslationErrorsResp 译文保存失败时的**逐行错误片段**（admin/partials/translation_errors.html）。
//
// 片段出口：保存失败就地替换 #page-translations-errors，不重渲整页 ——
// 页面与已填译文都没动过，重渲只会把一次校验失败变成一次全量查询。
type TranslationErrorsResp struct {
	// Errors 逐行错误：Label 是「哪一行」（原文 context 名，可空 = 页面级），
	// Msg 是词条事实 —— 两者在模板里合成一句。
	Errors []TranslationRowErrorResp
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

// TranslationRowErrorResp 逐行错误：Label 是「哪一行」（原文 context 名，可能为空 = 页面级），
// Msg 是结论事实。
//
// 不把 Label 拼进 Msg.Fallback：拼了就成成品串，模板再取词也救不回来（中文永远粘在英文词条前面）。
// 也不把 Label 塞进 Msg.Args —— 那个位置留给词条自身的 `%s` 参数。
type TranslationRowErrorResp struct {
	Label string
	Msg   facing.Msg
}
