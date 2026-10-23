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
