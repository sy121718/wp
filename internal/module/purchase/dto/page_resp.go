package purchasedto

import "go_wp/pkg/utils"

type OptionResp struct {
	Value string
	Label string
}

type ProjectOptionResp struct {
	ID   string
	Name string
}

type SourceRowResp struct {
	ID   string
	Code string
	Name string
	Type string

	TypeLabel         string
	RelatedParty      bool
	RelatedPartyLabel string
	Status            string
	StatusLabel       string

	SettlePrice    string
	HasSettlePrice bool
	Config         string
	Sort           int
	UpdatedAt      string
}

type SourceSummaryView struct {
	Total        int64
	Internal     int64
	External     int64
	RelatedParty int64
	Unrelated    int64
	SettlePriced int64
	Groups       []SourceSummaryGroupView
}

type SourceSummaryGroupView struct {
	Type              string
	TypeLabel         string
	RelatedParty      bool
	RelatedPartyLabel string
	Count             int64
}

type PurchaseOrderViewResp struct {
	ID   string
	Code string

	SourceName      string
	SourceType      string
	SourceTypeLabel string
	WarehouseName   string
	Status          string
	StatusLabel     string

	OrderedAt  utils.JSONTime
	ExpectedAt *utils.JSONTime
	Remark     string
	OperatorID string

	TotalQuantity       int
	ReceivedQuantity    int
	OutstandingQuantity int
	ProgressPercent     int

	OutstandingTone string

	ReceivedLabel    string
	OutstandingLabel string
	ProgressLabel    string
	Lines            []PurchaseOrderLineViewResp

	RequestID string
}

type PurchaseOrderLineViewResp struct {
	ID        string
	SKUCode   string
	VariantID string

	Quantity            int
	ReceivedQuantity    int
	OutstandingQuantity int
	UnitPrice           string
	Remark              string

	Open bool

	OutstandingTone string
}

type PurchaseHistoryViewResp struct {
	ReceiptCode     string
	Kind            string
	KindLabel       string
	OrderCode       string
	SourceName      string
	SourceTypeLabel string
	WarehouseName   string
	SKUCode         string
	Quantity        int
	UnitPrice       string
	CostUpdated     bool
	Remark          string
	OperatorID      string
	ReceivedAt      utils.JSONTime
}

type SourceOptionViewResp struct {
	ID    string
	Code  string
	Name  string
	Type  string
	Label string
}

type WarehouseOptionViewResp struct {
	ID        string
	Label     string
	IsDefault bool
}

type VariantOptionViewResp struct {
	VariantID string
	ProductID string
	SKUCode   string
	BareSKU   string
	Label     string
}

type DraftLineViewResp struct {
	Index     int
	SKU       string
	Quantity  string
	UnitPrice string
}

type SourceFormResp struct {
	IsEdit          bool
	Row             any
	SelectedProject string
	TypeOptions     []OptionResp
	RelatedOptions  []OptionResp
	StatusOptions   []OptionResp
}

type SourcePageResp struct {
	SourceCreateForm *SourceFormResp
}
