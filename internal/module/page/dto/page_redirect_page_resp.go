package pagedto

// page_redirect_page_resp.go — 重定向管理页的渲染视图（页面 GET 的回执形状）。
//
// 与 page_resp.go 的分工：那一份是接口契约；这一份是页面视图（模板直接读）。
// **导出字段名就是模板契约**：internal/templates/admin/page/page_redirects.html 里的
// `.Field` 必须在这里有同名导出字段。
//
// 这个页面原先用 gin.H 手写，且**自己注入 lang / t / csrf_token**（绕过 adminui.Prepare）。
// 换成视图后那些壳键仍由 Prepare 注入 —— 视图只带这一屏真正要的数据，
// 否则「哪些键是页壳给的、哪些是这一屏的」在类型上分不出来。

// RedirectPageResp 重定向管理整页的渲染视图。
//
// 全部字段都有零值可用：Jet 缺键会导致渲染器返回 500 通用错误页，
// 而本页有「取数失败也渲染完整壳」的分支 —— 那种分支下视图是零值 + ErrKey。
type RedirectPageResp struct {
	Title    string
	PagePath string

	// ErrKey 业务错误 → 词条 key（空串 = 无错误）。模板按 key 取词，不读成品句。
	ErrKey string

	// 新建表单的回显（失败时保留用户输入）。
	CreateSource string
	CreateTarget string
	CreateFailed bool

	Projects        []RedirectProjectOptionResp
	SelectedProject string
	Items           []RedirectItemResp
	Total           int
	EffectiveCount  int

	// 计数与分档成对（setRedirectCounts）：多跳 / 成环的档位跟计数走，
	// 分两处手写迟早漏一处 —— 那时页面上会同时出现「计数 3」和中性色。
	MultiHopCount int
	LoopCount     int
}
