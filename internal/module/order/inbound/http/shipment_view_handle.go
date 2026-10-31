package orderhttp

import (
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
)

type PendingShipPageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Rows  []*orderdto.PendingShipRow
	Total int64

	FilterKeyword string

	LoadFailed bool
	LoadError  string

	Page  int
	Limit int

	Base string
}

func (v PendingShipPageView) Paging() pagination.Page {
	return pagination.Page{Total: v.Total, Page: v.Page, Limit: v.Limit, Base: v.Base}
}

type CarrierListPageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Carriers []*orderdto.CarrierResp

	LoadFailed bool
	LoadError  string
}
