package orderhttp

import (
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

type OrderSalesPageResp struct {
	Title string
	Menu  string

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

type SalesTodoResp struct {
	AlertCount  int `view:"alertCount"`
	PendingShip int `view:"pendingShip"`
	Total       int `view:"total"`
}
