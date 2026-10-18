package inventorydto

import (
	"go_wp/pkg/utils"
)

// inventory_page_resp.go — 后台库存页的行元素。
//
// 只放「一行就是一条事实」的结构：库存页的两张表（某 SKU 的各仓库存、库存流水）。
// 页面数据根仍是 map（模板用 .["t"] 取词，Jet 的索引只对 map 生效），行元素一律结构体 ——
// 键名写错在结构体上是编译错误，在 map 上是「渲染成空白」且页面照样 200。

// StockByWarehouseRowResp 某 SKU 在各仓的库存，**按仓库展开**而不是只列已有的库存行。
//
// 「没有这一行」本身是一个状态（未入库）：靠「表里少一行」表达时，「所有仓都还没入库」
// 与「这个工程没有仓库」在界面上完全一样。Present 是这两者的分界。
type StockByWarehouseRowResp struct {
	// WarehouseID 行内编辑（登记外部编码 / 跟踪开关与数量）的定位键之一：
	// (仓库, 变体) 才唯一确定一行。展示用不上，但表单的隐藏域必须给出。
	WarehouseID   string
	WarehouseName string
	WarehouseCode string
	// Present 该仓有没有这条 SKU 的库存行（false = 未入库，没有可编辑对象）。
	Present   bool
	VariantID string
	SKUCode   string
	// ExternalSKU 这条货在该仓叫什么（迁移 251）。空串是合法状态 = 该仓用我们自己的 SKU。
	ExternalSKU string
	// TrackQuantity / Quantity 必须成对给出：只给数量的话，无限（track=false，数量恒 0）
	// 与卖光（track=true，数量 0）在页面上会是同一个 0。
	TrackQuantity bool
	Quantity      int
	// QuantityLabel 三态里的前两态已成文（∞ 无限 / 数字）；未入库由调用点给 notStocked 文案。
	QuantityLabel string
	UpdatedAt     string
}

// MovementRowResp 一行库存流水。
//
// 字段是这条流水的完整快照（含模板暂时没展示的 Remark / BatchID）：流水是审计，
// 少一列不影响渲染但会让「界面上看不到的那部分」无从追查。
type MovementRowResp struct {
	ID            string
	SKUCode       string
	WarehouseName string
	WarehouseCode string
	Direction     string
	// DirectionLabel 方向的中文（模板只用来显示，判断仍按 Direction）。
	DirectionLabel string
	Quantity       int
	QuantityBefore int
	QuantityAfter  int
	ReasonName     string
	ReasonCode     string
	SourceType     string
	SourceRef      string
	Remark         string
	OperatorID     string
	BatchID        string
	CreatedAt      string
}

// WarehouseRowResp 仓库行（仓库管理页的表格与各页的仓库下拉共用）。
//
// 第三方配置的各个键**恒存在**（非第三方仓给零值）：模板里直接取值不会因缺键中断 ——
// Jet 的缺失键会让整页从那一行起消失，而 HTTP 仍是 200（见 internal/templates/CLAUDE.md）。
// HasThirdParty 是「要不要显示那一组字段」的分界，不要靠 Provider 是否为空去猜。
type WarehouseRowResp struct {
	ID        string
	Code      string
	Name      string
	Type      string
	Status    string
	IsDefault bool
	Sort      int

	StatusLabel string
	TypeLabel   string

	// ThirdParty 第三方仓的对接配置（nil = 不是第三方仓）。
	//
	// 用接口面那个类型的指针而不是把字段拍平：拍平会让「凭据三件套」在页面 DTO 上出现第二份，
	// 而它们的安全论证（明文不落库、密文不出接口、只回布尔与掩码）写在 WarehouseThirdPartyResp 上 ——
	// 两份形状意味着两份论证，改一处不会让另一处报错。
	ThirdParty *WarehouseThirdPartyResp
}

// ReasonRowResp 变动原因一行（原因管理页表格与各页的原因下拉共用）。
//
// Name 是**已取词**的文案：name 列存的是 i18n key，页面永远不该出现
// inventory.reason.purchase_in 这样的裸 key；取不到词条时回退到 Code。
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

// OptionResp 下拉项：值 + 已取词的文案。
//
// 与 PickOptionResp（product 模块）刻意不共用：那边要表达「空项 / 数据项」，
// 这里的下拉没有空项语义，多出来的字段只会让人猜哪些键有意义。
type OptionResp struct {
	Value string
	Label string
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
