package orderdto

// order_shipment_page_resp.go — 发货三屏的页面视图（待发货列表 / 承运商配置）。
//
// 与 order_shipment_resp.go 的分工同 orders：那里是接口契约（JSON 出口，
// 带 json tag、字段名 camelCase），这里是**页面 GET 的回执形状**，
// 导出字段名就是模板契约（order_pending_ship.html 的 `.Rows` 等）。
//
// 为什么与 PendingShipPageResp 并存而不是复用它：接口出口只回答「这一页有哪些行、
// 一共多少」；页面还要页头的工程上下文、本次装载失败的原因、筛选回显与分页基址。
// 把那些塞进接口响应，等于让 JSON 调用方也收到页面的东西。

import (
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
)

// PendingShipPageView 待发货页整页的渲染视图。
type PendingShipPageView struct {
	Title string
	Menu  string

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Rows  []*PendingShipRow
	Total int64

	FilterKeyword string

	// LoadFailed / LoadError 本次装载失败（**不拿走整页**：空列表 + 归口提示 + 页壳）。
	// 两者并存是存量契约：模板用 LoadFailed 选分支，用 LoadError 显示原因；
	// 合并会同时动模板与三处调用点，与"改出口形态"分开做。
	LoadFailed bool
	LoadError  string

	Page  int
	Limit int
	// Base 翻页基址（带工程与关键词）：由控制器按请求算出，模板不拼 query。
	Base string
}

// Paging 分页事实（实现 view.PageList 即自动获得分页条）。
func (v PendingShipPageView) Paging() pagination.Page {
	return pagination.Page{Total: v.Total, Page: v.Page, Limit: v.Limit, Base: v.Base}
}

// CarrierListPageView 承运商配置页整页的渲染视图。
type CarrierListPageView struct {
	Title string
	Menu  string

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Carriers []*CarrierResp

	// LoadFailed / LoadError 同待发货页：装载失败不拿走整页。
	LoadFailed bool
	LoadError  string
}
