package pubdto

// publication_seo_audit_view.go — SEO 体检结果片段/整页的渲染视图。
//
// 这个端点一个 handler 服务三种出口（htmx 片段 / 非 htmx 的 JSON / 失败帧），
// 于是视图也分两份：成功帧带 Issues，失败帧只带错误提示。合成一个类型会让
// "失败帧要不要 Issues" 变成永远为 nil 的字段（模板读它不报错，但那是个假契约）。
//
// **导出字段名就是模板契约**：internal/templates/admin/analytics/seo_audit_result.html

// SeoAuditResultView 体检成功的渲染数据。
type SeoAuditResultView struct {
	AuditError bool
	Issues     []any
	Scanned    int
	Count      int
}

// SeoAuditErrorView 体检失败的渲染数据。
//
// AuditErrorText 是**已取好词的文本**：这个槽位在模板里直接渲染
// （{{.AuditErrorText}}），不取词。槽位还没取词化这件事登记在批次报告里，
// 而取词本身走 adminui.MsgText，不再经 shell。
type SeoAuditErrorView struct {
	AuditError     bool
	AuditErrorText string
}
