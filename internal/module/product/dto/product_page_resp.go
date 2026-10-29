// 商品预览、评分与外部查询契约沿用的对象。
package productdto

// WarehouseOptionResp 仓库身份、名称与默认仓事实；显示格式由模板决定。
type WarehouseOptionResp struct {
	ID        string
	Code      string
	Name      string
	IsDefault bool
}

// WarehouseSKUItemResp 「从仓库选」的一个候选货。
type WarehouseSKUItemResp struct {
	// SKUCode 一律是**裸码**（仓库侧的名字，不带仓码前缀）：前端把它填进「SKU 编码」框时，
	// 服务端按 (仓, 裸码) 复核并复用那一行 —— 带上前缀会被当成一条新货去建行。
	SKUCode string
	// ExternalSKU 该仓登记的对方编码（可能为空）。
	ExternalSKU string
	// StockID 是那条货行的 id：仓库明细按它认行（编码可以改，行 id 不会）。
	// 加行时带上它，服务端就不用按 (仓, 裸码) 再猜一次是哪一行。
	StockID string
	// Name / ImageURL 是迁移 642 的货品主档：商品表单的仓库明细按名称认货、带缩略图。
	// 空串 = 那条货还没填主档，不是读失败 —— 模板退化成显示编码。
	Name     string
	ImageURL string
	// Quantity / TrackQuantity 是选中之后的初值：数量框直接显示当前在库，
	// 不跟踪（无限）的行按 TrackQuantity 关掉输入框 —— 无限不是「0 件」。
	Quantity      int
	TrackQuantity bool
}

// WarehouseSKUGroupResp 一个仓的候选货集合（模板按仓分组渲染 optgroup / datalist）。
type WarehouseSKUGroupResp struct {
	WarehouseID string
	// WarehouseCode 仓短码：前端预览「将要生成的主体 SKU」用（服务端才是真源）。
	WarehouseCode string
	WarehouseName string
	IsDefault     bool
	Items         []WarehouseSKUItemResp
}

// EntityTemplateOptionResp 商品详情模板候选的一项（卡片区只展示名字与「默认」徽标）。
type EntityTemplateOptionResp struct {
	ID   string
	Name string
}
