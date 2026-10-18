package productdto

import json "go_wp/pkg/jsonx"

// product_bundle_req.go / product_bundle_resp.go — 捆绑品配置、选品校验与成员解析的入参与回执。
//
// BundleOption / BundleConfig 是**持久化形状**（直接落 products.bundle_items.options 的 JSON），
// 既进请求也进回执，所以它们是值对象而非入参/回执本身，留在 _req.go 里（请求是它们的源头）。

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

// BundleOptionDetailResp 选项 + SKU 快照 + 库存可用量。
type BundleOptionDetailResp struct {
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
	Options []*BundleOptionDetailResp `json:"options"`
}

// —— 整单校验（前台实时反馈 + 后端硬校验共用同一份入参与结论）——

// BundleSelectItemResp 一次选择里的一项（前台逐项输入数量）。
type BundleSelectItemResp struct {
	VariantID string `json:"variantId" form:"variantId"`
	Qty       int    `json:"qty" form:"qty"`
}

// BundleSelectedItemResp 校验通过后的展开结果（订单侧快照的形状：哪个 SKU、各多少）。
//
// 成员在套餐里**没有价格**：UnitPrice 恒为 0，套餐金额只算容器价（商品的默认价）。
// 这条是类型不变量（迁移 238）的一部分：成员只作为选项与履约/库存明细，成员原价只有
// 后台配置器可见（见 BundleOptionDetailResp.ItemPrice）。CostPrice 照旧保留，供后台毛利口径。
type BundleSelectedItemResp struct {
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

// BundleMemberResp 解析出的一条候选成员（尚未落库；前端把它追加进成员清单）。
type BundleMemberResp struct {
	VariantID   string `json:"variantId"`
	SKUCode     string `json:"skuCode"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	Enabled     bool   `json:"enabled"`
	// OptionValues 该变体的规格组合（jsonb 对象；前端据它展示可读规格）。
	OptionValues json.RawMessage `json:"optionValues"`
	// Source 来源快照（服务内部的形态，直接可落进 BundleOption 的四个字段）。
	// **不参与 JSON**：对外的四个键在下面平铺，见 SourceKind 的说明。
	Source BundleMemberSource `json:"-"`
	// 以下是来源快照的**平铺**形态：页面 JS 直接按这几个键取值（没有二次解包这一层）。
	// 嵌套一个 source 对象时前端要写 data.source.warehouseSku，而少写一层不会报错 ——
	// 只会拿到 undefined 并把成员来源存成空值。
	SourceKind   string `json:"sourceKind"`
	WarehouseID  string `json:"warehouseId,omitempty"`
	WarehouseSKU string `json:"warehouseSku,omitempty"`
	ExternalSKU  string `json:"externalSku,omitempty"`
	// Spec 规格的可读文本（「颜色 红 · 尺码 L」），由 service 按来源商品的属性组算好。
	Spec string `json:"spec"`
}

// BundleMemberSkipResp 本次没有加进来的那一条（不整批失败，逐条回带原因）。
//
// Reason 是 enums 常量（= i18n key），由 inbound 取词后展示；
// OptionValues 原样带回，供页面把它拼成可读规格文本（属性组合来源的「组合不存在」
// 就靠它指出来是哪一组）。
type BundleMemberSkipResp struct {
	VariantID    string          `json:"variantId,omitempty"`
	SKUCode      string          `json:"skuCode,omitempty"`
	WarehouseID  string          `json:"warehouseId,omitempty"`
	WarehouseSKU string          `json:"warehouseSku,omitempty"`
	OptionValues json.RawMessage `json:"optionValues,omitempty"`
	Reason       string          `json:"reason"`
	// Text 跳过原因的可读文本（已按当前语言取词）。
	Text string `json:"text"`
	// Label 这一行的可读标识（SKU / 仓库 SKU / 规格文本）。
	Label string `json:"label"`
}

// ResolveBundleMembersResp 来源解析结论：成功 N（Members）/ 跳过 M（Skipped）+ 逐条原因。
type ResolveBundleMembersResp struct {
	// OK 恒为 true（失败走 pkg/response 的错误体）：前端据它区分「解析过」与「没解析过」。
	OK        bool   `json:"ok"`
	ProductID string `json:"productId"`
	Source    string `json:"source"`
	// Members 本次解析出的候选成员（已去重，尚未落库）。
	Members []*BundleMemberResp `json:"members"`
	// Skipped 没有加进来的那几条（原因逐条可读；一条失败不影响其余）。
	// 键名是 skips：页面 JS 与回执同形（曾经这里叫 skipped，出口另拼一个 skips 数组，
	// 于是「跳过几条」有两个来源，一个是计数一个是数组）。
	Skipped []BundleMemberSkipResp `json:"skips"`
	// Total 去重前的候选总数（含被跳过与已存在的）。
	Total int `json:"total"`
}

// BundleSelectionResp 整单校验结论。
type BundleSelectionResp struct {
	ProductID string                    `json:"productId"`
	Items     []*BundleSelectedItemResp `json:"items"`
	TotalQty  int                       `json:"totalQty"`
	// TotalPrice 套餐价 = 主体自定价（子项价格不参与前台展示）。
	TotalPrice float64 `json:"totalPrice"`
	// TotalCost 子项成本合计（后台口径，前台不展示）。
	TotalCost float64 `json:"totalCost"`
}
