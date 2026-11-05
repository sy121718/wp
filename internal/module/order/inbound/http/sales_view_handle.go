package orderhttp

import (
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

type OrderSalesPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Todo SalesTodoResp

	Overview *orderdto.SalesOverviewResp

	FilterFrom   string
	FilterTo     string
	FilterStatus string
	Monthly      int

	StatusQuickValues []string

	OrderStatusLabel func(string) string

	LoadErr string

	ListQuery string
}

// SalesTodoResp 是 OrderSalesPageResp.Todo 的字段**值**，模板按字段名访问
// （.Todo.Total / .Todo.AlertCount / .Todo.PendingShip）。
// 这里不写 view tag：adminui.ViewMap 只转换顶层结构的字段，嵌套值仍是原生结构体，
// Jet 对原生结构体只认字段名 —— 加 tag 会让下一个人误以为能写小写。
type SalesTodoResp struct {
	AlertCount  int
	PendingShip int
	Total       int
}
