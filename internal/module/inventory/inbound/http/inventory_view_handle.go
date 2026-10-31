package inventoryhttp

import (
	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/pkg/pagination"
)

type StockByWarehouseRowResp struct {
	WarehouseID   string
	WarehouseName string
	WarehouseCode string

	Present   bool
	VariantID string
	SKUCode   string

	ExternalSKU string

	TrackQuantity bool
	Quantity      int

	ReservedQuantity  int
	AvailableQuantity int

	QuantityLabel string
	UpdatedAt     string
}

type MovementRowResp struct {
	ID      string
	StockID string
	SKUCode string

	StockName     string
	StockImageURL string
	WarehouseName string
	WarehouseCode string
	Direction     string

	DirectionLabel string
	Quantity       int

	Delta          int
	QuantityBefore int
	QuantityAfter  int
	ReasonName     string
	ReasonCode     string
	SourceType     string
	SourceRef      string
	Remark         string
	OperatorID     string
	BatchID        string

	UnitCostText string
	CreatedAt    string
}

type WarehouseRowResp struct {
	ID        string
	Code      string
	Name      string
	Type      string
	Status    string
	IsDefault bool
	Sort      int

	DeductPriority int

	StatusLabel string
	TypeLabel   string

	ThirdParty *inventorydto.WarehouseThirdPartyResp
}

type ReasonRowResp struct {
	ID             string
	Code           string
	Name           string
	Direction      string
	DirectionLabel string
	IsBuiltin      bool
	Status         string
	StatusLabel    string
	Sort           int
}

type OptionResp struct {
	Value string
	Label string
}

type VariantOptionResp struct {
	VariantID string
	ProductID string
	SKUCode   string
	Label     string
}

type InventoryPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []ProjectOptionResp
	SelectedProject string

	Warehouses      []WarehouseRowResp
	VariantOptions  []VariantOptionResp
	SelectedSKU     string
	SelectedVariant string
	StockRows       []StockByWarehouseRowResp
	Reasons         []ReasonRowResp

	AdjustReasons    []ReasonRowResp
	AdjustDirections []OptionResp
	Directions       []OptionResp
	Movements        []MovementRowResp

	FilterSKU       string
	FilterWarehouse string
	FilterDirection string
	FilterReason    string
	FilterTimeFrom  string
	FilterTimeTo    string

	ListQuery string

	Pagination pagination.Page

	WarehouseTypes      []WarehouseTypeOptionResp
	WarehouseCreateForm *WarehouseFormResp

	ReasonCreateForm *ReasonFormResp
}

type ProjectOptionResp struct {
	ID   string
	Name string
}

type TransferListPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []ProjectOptionResp
	SelectedProject string
	Warehouses      []WarehouseRowResp

	Rows          []*inventorydto.TransferRowResp
	FilterStatus  string
	Keyword       string
	StatusOptions []OptionResp

	ListQuery  string
	Pagination pagination.Page
}

type StockSKUListPageResp struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []ProjectOptionResp
	SelectedProject string
	Warehouses      []WarehouseRowResp

	Rows            []inventorydto.StockSKURowResp
	FilterWarehouse string
	FilterKeyword   string
	FilterStatus    string
	StatusOptions   []OptionResp

	ListQuery  string
	Pagination pagination.Page
}
