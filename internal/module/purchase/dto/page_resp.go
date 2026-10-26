// page_resp.go — 采购域页面（采购入库页 / 货源管理页）的模板视图类型。
//
// 这些类型原本在 inventory/dto/inventory_page_resp.go 里，2026-10 随采购域迁入
// purchase 模块。dto 不跨模块依赖（module_boundary_test.go），所以 OptionResp /
// ProjectOptionResp 两个通用小类型在这里另存一份镜像，而不是复用库存那份 ——
// 复用会让两个模块在同一张页面上互相拽着走。
package purchasedto

import "go_wp/pkg/utils"

// OptionResp 下拉项：值 + 已取词的文案。
type OptionResp struct {
	Value string
	Label string
}

// ProjectOptionResp 工程下拉的一项（本包镜像：只带模板读的两项）。
type ProjectOptionResp struct {
	ID   string
	Name string
}

// SourceRowResp 货源一行（金额与配置已格式化，模板不做逻辑）。
type SourceRowResp struct {
	ID   string
	Code string
	Name string
	Type string
	// TypeLabel / RelatedPartyLabel / StatusLabel 是已取词的展示文案，判断仍按 Type / Status。
	TypeLabel         string
	RelatedParty      bool
	RelatedPartyLabel string
	Status            string
	StatusLabel       string
	// SettlePrice 两位小数的金额；未设置时是 "—"（HasSettlePrice 为假才允许这样显示）。
	SettlePrice    string
	HasSettlePrice bool
	Config         string
	Sort           int
	UpdatedAt      string
}

// SourceSummaryView 关联方统计（货源页顶部卡）。
//
// Groups 在装载失败时是空切片而不是 nil：模板读的是 len(...)，两者等价；
// 但 nil 一旦被当成「没这项」就会导致模板里别的键被跳过，所以这里恒给非 nil。
type SourceSummaryView struct {
	Total        int64
	Internal     int64
	External     int64
	RelatedParty int64
	Unrelated    int64
	SettlePriced int64
	Groups       []SourceSummaryGroupView
}

// SourceSummaryGroupView 统计里的一组（类型 + 关联方）。
type SourceSummaryGroupView struct {
	Type              string
	TypeLabel         string
	RelatedParty      bool
	RelatedPartyLabel string
	Count             int64
}

// PurchaseOrderViewResp 采购单一行（列表页，含逐行明细与一次性幂等键）。
type PurchaseOrderViewResp struct {
	ID   string
	Code string

	SourceName      string
	SourceType      string
	SourceTypeLabel string
	WarehouseName   string
	Status          string
	StatusLabel     string

	// OrderedAt / ExpectedAt 保持 utils.JSONTime：模板直接渲染时间字段，
	// 转成字符串就丢掉了「对外 JSON 只到秒」的那份口径（见 AGENTS.md 时间列约定）。
	OrderedAt  utils.JSONTime
	ExpectedAt *utils.JSONTime
	Remark     string
	OperatorID string

	TotalQuantity    int
	ReceivedQuantity int
	Lines            []PurchaseOrderLineViewResp
	// RequestID 每张单一个一次性幂等键（渲染表单时生成，重复提交命中同一张入库单）。
	RequestID string
}

// PurchaseOrderLineViewResp 采购单的一行明细。
type PurchaseOrderLineViewResp struct {
	ID        string
	SKUCode   string
	VariantID string

	Quantity            int
	ReceivedQuantity    int
	OutstandingQuantity int
	UnitPrice           string
	Remark              string
	// Open 还能收的行才给提交按钮：收满的行不再受理（服务端同样拒绝）。
	Open bool
	// OutstandingTone 未收数量的展示色调（模板按类名直出）。
	OutstandingTone string
}

// PurchaseHistoryViewResp 进货历史一行（当前页面不渲染这张表，接口与将来的入口共用同一投影）。
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

// SourceOptionViewResp 货源下拉项（Label 是「名称（编码）· 类型」的成品串）。
type SourceOptionViewResp struct {
	ID    string
	Code  string
	Name  string
	Type  string
	Label string
}

// WarehouseOptionViewResp 收货仓下拉项（默认仓在 Label 里标出来，IsDefault 供别的判断用）。
type WarehouseOptionViewResp struct {
	ID        string
	Label     string
	IsDefault bool
}

// VariantOptionViewResp 采购行 SKU 下拉项。
//
// BareSKU 是**仓库侧裸码**（inventory_stocks.sku_code）：采购行的 sku_code 必须按裸码落库，
// 仓库里的 SKU 永远不带仓码前缀。没有库存行的变体退回商品侧编码，由服务端在入库入口归一。
type VariantOptionViewResp struct {
	VariantID string
	ProductID string
	SKUCode   string
	BareSKU   string
	Label     string
}

// DraftLineViewResp 新建采购单表单里的一个行槽位（固定几行原生表单）。
//
// 三个值来自失败重渲时的**原始提交值**（不修剪、不剔空行）：修剪会让行号与用户看到的错位。
type DraftLineViewResp struct {
	Index     int
	SKU       string
	Quantity  string
	UnitPrice string
}

// SourceFormResp 货源抽屉表单的初值（新建与编辑共用）。
type SourceFormResp struct {
	IsEdit          bool
	Row             any
	SelectedProject string
	TypeOptions     []OptionResp
	RelatedOptions  []OptionResp
	StatusOptions   []OptionResp
}

// SourcePageResp 货源管理整页：新建抽屉的初值挂在页数据上（页数据本体在
// inbound/http 的 sourcesPageData —— 它与降级分支共用同一份装配）。
//
// 单字段而不是把页数据也搬进 dto：那个结构体在两条渲染路径之间周转，
// 搬过来的收益只是「少一个文件内的类型」，代价是 dto 里长出一堆只服务一页的字段。
type SourcePageResp struct {
	SourceCreateForm *SourceFormResp
}
