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
type BundleSelectedItem struct {
	VariantID   string   `json:"variantId"`
	SKUCode     string   `json:"skuCode"`
	ProductName string   `json:"productName"`
	Qty         int      `json:"qty"`
	UnitPrice   float64  `json:"unitPrice"`
	CostPrice   *float64 `json:"costPrice"`
	Available   int      `json:"available"`
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
