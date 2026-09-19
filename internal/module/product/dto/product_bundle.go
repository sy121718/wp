// product_bundle.go — 捆绑品选项规则与整单校验（issue #20）。
//
// 捆绑品的语义（spec 议题 #2 §捆绑与关联）：主体只有一个价格（运营自定），
// 子项不单独展示价格（成本仍在后台保留），库存按 BOM 展开扣减。本票补的是
// 「选项规则 + 数量 + 校验 + 实时算价」这一层。
//
// 存储形状（products.bundle_items，见迁移 114）：
//
//	{maxOptions, minTotalQty, maxTotalQty, options: [{variantId, required, defaultQty, minQty, maxQty}]}
//
// maxQty / maxTotalQty 的 0 = 留空（不设上限，只受库存约束）；数量取值域恒 >= 0，
// 用 0 表示「不设上限」比 null 少一层分支。
package productdto

import "encoding/json"

// —— 服务端护栏常量 ——

const (
	// BundleMaxOptionsLimit 选项数量硬上限。后台的 maxOptions 是运营可调的上限，
	// 但不得超过本值 —— 护栏设在服务端，不依赖前端表单的 maxlength。
	BundleMaxOptionsLimit = 20
	// BundleDefaultMaxOptions 新建捆绑配置时的默认选项上限。
	BundleDefaultMaxOptions = 20
	// BundleMaxQtyLimit 单项 / 整单数量的硬上限（防离谱值撑爆计算与库存查询）。
	BundleMaxQtyLimit = 9999
)

// BundleOption 一个选项的规则（直接落在 products.bundle_items.options 里）。
//
// VariantID 指向**任意商品的已存在 SKU**（跨商品挑选，spec 第 53/54 条）。
type BundleOption struct {
	VariantID string `json:"variantId"`
	// Required 必选 / 可选：必选项在整单校验里必须出现且满足 MinQty。
	Required bool `json:"required"`
	// DefaultQty 前台初始数量（必选项 >= MinQty；可选项可为 0）。
	DefaultQty int `json:"defaultQty"`
	// MinQty 单项最小数量（必选项 >= 1；可选项可为 0，即允许不选）。
	MinQty int `json:"minQty"`
	// MaxQty 单项最大数量；0 = 留空（不设上限，只受库存约束）。
	MaxQty int `json:"maxQty"`

	// —— 成员来源快照（docs/14 §1.2 的三种来源：迁移 259 配套）——
	//
	// **仅作溯源与展示，不是身份**：成员的身份恒为 VariantID（uuid），
	// 仓库那串编码只是「这条货在这个仓叫什么」（§9.3），仓库换码不影响成员的引用。
	// 手工指定的历史配置没有这段快照（全空串），照常可用。
	//
	// SourceKind 取 productenums.BundleSource*（空 = 手工指定 / 历史配置）。
	SourceKind string `json:"sourceKind,omitempty"`
	// WarehouseID / WarehouseSKU 来自「从仓库选」那条来路（仓储侧的仓与仓内 SKU）。
	WarehouseID  string `json:"warehouseId,omitempty"`
	WarehouseSKU string `json:"warehouseSku,omitempty"`
	// ExternalSKU 该 (仓库, 变体) 库存行登记的外部 / 第三方编码（迁移 251，可为空）。
	ExternalSKU string `json:"externalSku,omitempty"`
}

// BundleConfig 捆绑品配置（products.bundle_items 的完整形状）。
type BundleConfig struct {
	// MaxOptions 选项数量上限（1..BundleMaxOptionsLimit）。
	MaxOptions int `json:"maxOptions"`
	// MinTotalQty 整单最小总件数（所有选项数量之和的下限；0 = 不设下限）。
	MinTotalQty int `json:"minTotalQty"`
	// MaxTotalQty 整单最大总件数（0 = 留空，不设上限）。
	MaxTotalQty int            `json:"maxTotalQty"`
	Options     []BundleOption `json:"options"`
}

// NewEmptyBundleConfig 空配置（与迁移 114 的列默认值同形状）。
func NewEmptyBundleConfig() BundleConfig {
	return BundleConfig{MaxOptions: BundleDefaultMaxOptions, Options: []BundleOption{}}
}

// —— 后台配置 ——

// GetBundleConfigReq 读取某商品的捆绑配置。
type GetBundleConfigReq struct {
	ProductID string `form:"productId" json:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `form:"projectId" json:"projectId"`
}

// SetBundleConfigReq 保存某商品的捆绑配置（整体替换）。
//
// OperatorID 由 inbound 从会话覆盖写入（客户端传入被忽略）：配置变更要落主数据变更记录。
type SetBundleConfigReq struct {
	ProductID string `form:"productId" json:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID  string       `form:"projectId" json:"projectId"`
	Config     BundleConfig `json:"config"`
	OperatorID string       `json:"-" form:"-"`
}

// ListBundleSKUReq 可挑选的 SKU 清单（后台配置器的下拉数据源）。
type ListBundleSKUReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
}

// BundleSKUResp 可挑选的 SKU（跨商品），带所属商品与后台可见的价格 / 成本。
type BundleSKUResp struct {
	VariantID   string   `json:"variantId"`
	SKUCode     string   `json:"skuCode"`
	ProductID   string   `json:"productId"`
	ProductName string   `json:"productName"`
	Price       float64  `json:"price"`
	CostPrice   *float64 `json:"costPrice"`
	Enabled     bool     `json:"enabled"`
}

// —— 配置详情（后台 / 前台配置器共用的读模型）——

// BundleOptionDetail 选项 + SKU 快照 + 库存可用量。
type BundleOptionDetail struct {
	BundleOption
	SKUCode     string `json:"skuCode"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	// ItemPrice / CostPrice 只在后台与订单侧使用，前台不展示子项价格。
	ItemPrice float64  `json:"itemPrice"`
	CostPrice *float64 `json:"costPrice"`
	// Available SKU 的可用量：只读 inventory 真源（绝不读 product_variants.stock_total 缓存）。
	Available int  `json:"available"`
	Enabled   bool `json:"enabled"`
}

// BundleConfigResp 捆绑配置详情（后台配置页 / 前台配置器同一读模型）。
type BundleConfigResp struct {
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	// BasePrice 套餐价 = 主体自定价（商品的默认价，缺失时取最低启用变体价）。
	BasePrice float64      `json:"basePrice"`
	Config    BundleConfig `json:"config"`
	// Options 配置里的选项，按配置顺序返回；SKU 已删除或停用时仍返回（Enabled=false），
	// 由后台提示运维修正，而不是静默丢项。
	Options []*BundleOptionDetail `json:"options"`
}

// —— 整单校验（前台实时反馈 + 后端硬校验共用同一份入参与结论）——

// BundleSelectItem 一次选择里的一项（前台逐项输入数量）。
type BundleSelectItem struct {
	VariantID string `json:"variantId" form:"variantId"`
	Qty       int    `json:"qty" form:"qty"`
}

// ValidateBundleSelectionReq 整单校验请求。
//
// Items 是**整单的完整选择**（不是增量）：漏填的必选项在这里就是缺席，
// 校验因此是纯函数式的 —— 同样的选择永远得到同样的结论。
type ValidateBundleSelectionReq struct {
	ProductID string `json:"productId" form:"productId"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string             `json:"projectId" form:"projectId"`
	Items     []BundleSelectItem `json:"items" form:"-"`
}

// BundleSelectedItem 校验通过后的展开结果（订单侧快照的形状：哪个 SKU、各多少）。
//
// 成员在套餐里**没有价格**：UnitPrice 恒为 0，套餐金额只算容器价（商品的默认价）。
// 这条是类型不变量（迁移 238）的一部分：成员只作为选项与履约/库存明细，成员原价只有
// 后台配置器可见（见 BundleOptionDetail.ItemPrice）。CostPrice 照旧保留，供后台毛利口径。
type BundleSelectedItem struct {
	VariantID   string  `json:"variantId"`
	SKUCode     string  `json:"skuCode"`
	ProductName string  `json:"productName"`
	Qty         int     `json:"qty"`
	UnitPrice   float64 `json:"unitPrice"`
	// MemberPrice 成员自己的挂牌价（参考值，**禁止参与任何金额计算**）：
	// 后台看毛利/核对配置时要用，但套餐金额只能由容器价得出。
	// 需要成员价的地方一律显式读这个字段，读 UnitPrice 只会拿到 0。
	MemberPrice float64  `json:"memberPrice"`
	CostPrice   *float64 `json:"costPrice"`
	Available   int      `json:"available"`
}

// —— 捆绑成员的三种来源（docs/14 §1.2，批次 C）——
//
// 与变体清单的「预览—保存」模型同一形态（docs/14 §8）：解析**不落库**，
// 只把候选行交回前端清单；点「保存配置」（SetBundleConfig）才写入 products.bundle_items。
//
// 三种来源共用一个出口，因此去重口径、跳过口径、上限口径都只有一份：
//
//	① product    —— 选中一个商品 → 其全部**启用**变体一次导入为成员（VariantDisabled 的跳过）；
//	② warehouse  —— 按仓给出仓库 SKU（我们自己的编码），选中即定位到该仓那条货的变体，
//	                并记下来源快照（warehouseId / warehouseSku / externalSku）；
//	③ attributes —— 勾选属性值 → **服务端按属性组固定顺序重算笛卡尔积**，
//	                只接受「商品侧确实存在对应变体」的组合；不存在的组合明确拒绝
//	                并逐条回带原因（BundleMemberNotOnProduct，提示先去商品上生成该规格的变体）。
type ResolveBundleMembersReq struct {
	// ProductID 捆绑容器（成员挂到它身上）。
	ProductID string `json:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
	// Source 来源（productdto 之外只认 productenums.BundleSource* 三个值）。
	Source string `json:"source"`
	// SourceProductID 来源商品（product / attributes 两种来源必填）。
	SourceProductID string `json:"sourceProductId"`
	// WarehouseID / WarehouseSKUs 仓库来源：仓 + 该仓的仓库 SKU 编码（服务端按
	// (仓库, SKU) 复核，前端提交的只是线索 —— 与新建商品「从仓库选」同一口径）。
	WarehouseID   string   `json:"warehouseId"`
	WarehouseSKUs []string `json:"warehouseSkus"`
	// Selections 属性组合来源的勾选（属性组 id → 属性值 id，多值）。
	Selections []VariantSelectionReq `json:"selections"`
	// ExistingVariantIDs 前端清单里已有的成员变体 id：服务端据此去重（同一变体只出现一次），
	// 前端提交的形状一律不作数 —— 它只影响「哪些行不再追加」。
	ExistingVariantIDs []string `json:"existingVariantIds"`
}

// BundleMemberDraft 解析出的一条候选成员（尚未落库；前端把它追加进成员清单）。
type BundleMemberDraft struct {
	VariantID   string `json:"variantId"`
	SKUCode     string `json:"skuCode"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	Enabled     bool   `json:"enabled"`
	// OptionValues 该变体的规格组合（jsonb 对象；前端据它展示可读规格）。
	OptionValues json.RawMessage `json:"optionValues"`
	// Source 来源快照（直接可落进 BundleOption 的四个字段）。
	Source BundleMemberSource `json:"source"`
}

// BundleMemberSource 成员来源快照（不是身份，见 BundleOption 的说明）。
type BundleMemberSource struct {
	Kind         string `json:"kind"`
	WarehouseID  string `json:"warehouseId,omitempty"`
	WarehouseSKU string `json:"warehouseSku,omitempty"`
	ExternalSKU  string `json:"externalSku,omitempty"`
}

// BundleMemberSkip 本次没有加进来的那一条（不整批失败，逐条回带原因）。
//
// Reason 是 enums 常量（= i18n key），由 inbound 取词后展示；
// OptionValues 原样带回，供页面把它拼成可读规格文本（属性组合来源的「组合不存在」
// 就靠它指出来是哪一组）。
type BundleMemberSkip struct {
	VariantID    string          `json:"variantId,omitempty"`
	SKUCode      string          `json:"skuCode,omitempty"`
	WarehouseID  string          `json:"warehouseId,omitempty"`
	WarehouseSKU string          `json:"warehouseSku,omitempty"`
	OptionValues json.RawMessage `json:"optionValues,omitempty"`
	Reason       string          `json:"reason"`
}

// ResolveBundleMembersResp 来源解析结论：成功 N（Members）/ 跳过 M（Skipped）+ 逐条原因。
type ResolveBundleMembersResp struct {
	ProductID string `json:"productId"`
	Source    string `json:"source"`
	// Members 本次解析出的候选成员（已去重，尚未落库）。
	Members []*BundleMemberDraft `json:"members"`
	// Skipped 没有加进来的那几条（原因逐条可读；一条失败不影响其余）。
	Skipped []BundleMemberSkip `json:"skipped"`
	// Total 去重前的候选总数（含被跳过与已存在的）。
	Total int `json:"total"`
}

// BundleSelectionResp 整单校验结论。
type BundleSelectionResp struct {
	ProductID string                `json:"productId"`
	Items     []*BundleSelectedItem `json:"items"`
	TotalQty  int                   `json:"totalQty"`
	// TotalPrice 套餐价 = 主体自定价（子项价格不参与前台展示）。
	TotalPrice float64 `json:"totalPrice"`
	// TotalCost 子项成本合计（后台口径，前台不展示）。
	TotalCost float64 `json:"totalCost"`
}
