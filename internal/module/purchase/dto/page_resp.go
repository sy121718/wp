package purchasedto

import "go_wp/pkg/utils"

// OptionResp 是纯选项事实：只保值和顺序。
// 显示标签由模板按请求语言取词，不在 DTO 里预先翻译。
// OptionResp 是纯选项事实：只保值，保持后端枚举顺序。
// 显示标签（含词条 key 与兜底）是固定 UI 映射，写在模板里，按请求语言取词。
type OptionResp struct {
	Value string
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

	RelatedParty bool
	Status       string

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
	Type         string
	RelatedParty bool
	Count        int64
}

type PurchaseOrderViewResp struct {
	ID   string
	Code string

	SourceName    string
	SourceType    string
	WarehouseName string
	Status        string

	OrderedAt  utils.JSONTime
	ExpectedAt *utils.JSONTime
	Remark     string
	OperatorID string

	TotalQuantity       int
	ReceivedQuantity    int
	OutstandingQuantity int
	ProgressPercent     int

	OutstandingTone string

	ProgressLabel string
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
	OrderCode       string
	SourceName      string
	SourceType      string
	WarehouseName   string
	SKUCode         string
	Quantity        int
	UnitPrice       string
	CostUpdated     bool
	Remark          string
	OperatorID      string
	ReceivedAt      utils.JSONTime
}

// SourceOptionViewResp 只交货源事实；选项文案由模板按请求语言组合。
type SourceOptionViewResp struct {
	ID   string
	Code string
	Name string
	Type string
}

// WarehouseOptionViewResp 只交仓库事实；「默认仓」等文案由模板取词。
type WarehouseOptionViewResp struct {
	ID        string
	Name      string
	Code      string
	IsDefault bool
}

// VariantOptionViewResp 只交变体事实；成本说明等文案由模板取词。
type VariantOptionViewResp struct {
	VariantID string
	ProductID string
	SKUCode   string
	BareSKU   string
	Label     string
	Cost      string
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
