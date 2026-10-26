package inventorydto

// inventory_warehouse_sku_page_resp.go — 仓库商品 SKU 台账页（迁移 642 / B3）的读模型。
//
// 这一屏回答的是「每个仓里有哪些货、叫什么、还剩多少、挂在哪个变体上」——
// 它是「从仓库选」的反向视角：候选下拉只列能选的，台账页要把**全部**行列出来
//（包括停用的、还没被任何商品引用的），否则运营没法判断「这批货到底建过没有」。
//
// 命名约定：Text 后缀的字段是**已定型的字符串**（金额、数量、币对、时间），
// 不是词条 key —— 词条（状态文案、未固化提示）仍由模板自己取，所以这里没有
// StatusLabel 这类展示字段（presentation_owner_test 的禁名单）。

// StockVariantUsage 一条货行被某个变体引用的事实（台账页「被哪些变体用着」）。
//
// 只带标识与编码：页面上只显示「这条货挂在哪个变体上」，点进去是商品页的事。
type StockVariantUsage struct {
	VariantID string
	SKUCode   string
}

// StockSKURowResp 台账页一行 = 一条货行（(仓库, 仓库 SKU)）。
//
// 与 StockResp 的分工：StockResp 是接口出口（带 projectId / metadata 这类调用方要用的字段），
// 本形状只放这一屏要显示的东西，并额外带上「被几个变体引用」——
// 那条关系在商品侧（product_variants.warehouse_skus），页面上必须能看见，
// 否则运营看到一行有货却不知道为什么商品列表里没它。
type StockSKURowResp struct {
	ID            string
	WarehouseID   string
	WarehouseName string
	WarehouseCode string
	IsDefault     bool

	SKUCode     string
	ExternalSKU string
	Barcode     string
	Name        string
	ImageURL    string

	// QuantityText 跟踪时的数量文本；**不跟踪时为空串**（模板据 TrackQuantity 渲染
	// 「∞ + 无限」词条）—— 不跟踪的行数量恒 0，直接显示 0 会让「无限」看起来像「没货」。
	QuantityText      string
	ReservedQuantity  int
	AvailableQuantity int
	TrackQuantity     bool

	// CostText / FreightText 已成文（未核算时是 —，不是空白）。
	CostText    string
	FreightText string
	// CurrencyText 成本币种与基准币不同时显示「USD → CNY」，相同时只显示币种。
	CurrencyText string
	// FXText 汇率文本（同币种时为空串）。
	FXText string
	// FXMissing 两个币种不同却没有汇率 —— 模板据此显示「汇率未固化」提示词条，
	// 而不是留白或按 1:1 猜一个能算的数（dto 不取词，只说事实）。
	FXMissing bool

	Status string

	// VariantCount 这条货被多少个变体引用着（0 = 仓库里有货、商品侧还没挂）。
	VariantCount int
	// Variants 引用它的变体（最多列 UsageLimit 条；超出的靠 VariantCount 表达）。
	Variants []StockVariantUsage

	Remark    string
	UpdatedAt string
}

// StockSKUPageReq 仓库商品 SKU 台账页的筛选参数（页面用 project / page / limit，与库存页同一套键）。
//
// 不复用 ListWarehouseSKUReq：那是**接口**入参（form:"projectId" / size），
// 台账页的地址栏键是已发布 URL 的一部分（project / page / limit），改名会废掉书签。
type StockSKUPageReq struct {
	ProjectID   string `form:"project"`
	WarehouseID string `form:"warehouseId"`
	Keyword     string `form:"keyword"`
	// Status 为空 = 不限（台账要能看见停用的行）；"active" / "disabled" = 只看某一档。
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}
