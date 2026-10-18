package inventorydto

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

	StatusLabel   string
	TypeLabel     string
	HasThirdParty bool

	Provider         string
	ExternalCode     string
	Address          string
	Contact          string
	AllowsShipping   bool
	HasCredential    bool
	CredentialMasked string
	SecretRef        string
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
