package inventoryhttp

type WarehouseTypeOptionResp struct {
	Value string
	Label string
}

type WarehousesPageResp struct {
	Title           string
	Menu            string
	Projects        []any
	SelectedProject string

	Warehouses          []WarehouseRowResp
	WarehouseTypes      []WarehouseTypeOptionResp
	WarehouseCreateForm *WarehouseFormResp

	ListQuery string
}

type WarehouseFormResp struct {
	IsEdit          bool
	Row             *WarehouseRowResp
	SelectedProject string
	WarehouseTypes  []WarehouseTypeOptionResp
}

type ReasonFormResp struct {
	SelectedProject string
	Directions      []any
}
