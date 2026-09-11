// inventory_resp.go — inventory 模块出参。
package inventorydto

// WarehouseResp 仓库（含默认仓标记）。
type WarehouseResp struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	IsDefault bool   `json:"isDefault"`
	Sort      int    `json:"sort"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// StockResp 库存记录（带上仓库展示信息，后台不必二次查询）。
//
// Quantity 是**真源**可用量：直接读 inventory_stocks.quantity，
// 绝不读 product_variants.stock_total（那是列表展示用的冗余缓存）。
type StockResp struct {
	ID            string `json:"id"`
	ProjectID     string `json:"projectId"`
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	ProductID     string `json:"productId"`
	VariantID     string `json:"variantId"`
	SKUCode       string `json:"skuCode"`
	Quantity      int    `json:"quantity"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}
