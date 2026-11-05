package orderhttp

import (
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/pagination"
)

type StatusTabResp struct {
	Value string
	Kind  string
}

type OrderListPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Orders []*orderdto.OrderResp
	Counts map[string]int64
	Total  int64
	Detail *orderdto.OrderDetailResp

	StatusTabs []StatusTabResp
	CountsAll  int64

	OrderStatusLabel func(string) string

	CountryLabel func(string) string

	FilterStatus  string
	FilterKeyword string
	FilterPayment string

	FulfillmentTabs []StatusTabResp

	FulfillmentCounts map[string]int64

	FulfillmentLabels map[string]StatusLabelFact
	FilterFulfillment string

	CreatedViaValues []string
	FilterCreatedVia string

	CreatedViaLabels map[string]StatusLabelFact

	LoadErr string

	ListQuery string

	Page  int
	Limit int

	Pagination pagination.Page
}

type CouponListPageResp struct {
	Title           string `view:"title"`
	Menu            string `view:"menu"`
	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Coupons []*orderdto.CouponResp
	Total   int64

	Expanded        *orderdto.CouponResp
	Redemptions     []*orderdto.CouponRedemptionResp
	RedemptionTotal int64
	RedemptionLimit int

	StatusValues      []string
	StateLabels       map[string]StatusLabelFact
	FilterStatus      string
	FilterStatusValid bool
	FilterKeyword     string

	LoadErr string

	ListQuery string

	Page  int
	Limit int

	Pagination pagination.Page
}

type CouponEditFormView struct {
	Coupon    *orderdto.CouponResp
	ListQuery string
}

type StatusLabelFact struct {
	Key      string
	Fallback string
}

type ReturnWarehouseResp struct {
	ID   string
	Name string
}

type ReturnListPageResp struct {
	Title           string `view:"title"`
	Menu            string `view:"menu"`
	Projects        []projectcontract.ProjectResp
	SelectedProject string

	Returns []*orderdto.ReturnResp
	Counts  map[string]int64
	Total   int64

	CountsAll int64
	Detail    *orderdto.ReturnDetailResp

	StatusTabs []StatusTabResp

	ReturnStatusLabels map[string]StatusLabelFact
	OrderStatusLabels  map[string]StatusLabelFact

	FilterStatus  string
	FilterKeyword string
	FilterOrderID uint64

	LoadErr string

	ListQuery string

	Warehouses []ReturnWarehouseResp

	CountryLabel func(string) string

	Page  int
	Limit int

	Pagination pagination.Page
}

type OrderNewPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectcontract.ProjectResp
	SelectedProject string

	LoadFailed bool
	NoProject  bool
	FormReady  bool

	CandidateKeyword string
	Candidates       []SkuOptionResp
	CandidateCount   int
	CandidateError   string

	ItemRows []OrderNewItemRowResp
	MaxRows  int

	ShipCountries []CountryOptionResp
	BillCountries []CountryOptionResp

	CurrencyOptions []CountryOptionResp

	RowAddURL string

	Err string

	FieldErrors   map[string]string
	HasFieldError bool

	Form any
	Echo any
}

type SkuOptionResp struct {
	ID    string
	Label string
}

type CountryOptionResp struct {
	Code     string
	Label    string
	Selected bool
}

type OrderNewItemRowResp struct {
	VariantID string
	Quantity  string
}
