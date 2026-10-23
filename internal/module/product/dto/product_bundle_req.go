package productdto

import (
	"net/url"
	"strings"

	json "go_wp/pkg/jsonx"
)

// product_bundle_req.go / product_bundle_resp.go — 捆绑品配置、选品校验与成员解析的入参与回执。
//
// BundleOption / BundleConfig 是**持久化形状**（直接落 products.bundle_items.options 的 JSON），
// 既进请求也进回执，所以它们是值对象而非入参/回执本身，留在 _req.go 里（请求是它们的源头）。

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
	VariantID string `json:"variantId" form:"variantId"`
	// Required 必选 / 可选：必选项在整单校验里必须出现且满足 MinQty。
	Required bool `json:"required" form:"required"`
	// DefaultQty 前台初始数量（必选项 >= MinQty；可选项可为 0）。
	DefaultQty int `json:"defaultQty" form:"defaultQty"`
	// MinQty 单项最小数量（必选项 >= 1；可选项可为 0，即允许不选）。
	MinQty int `json:"minQty" form:"minQty"`
	// MaxQty 单项最大数量；0 = 留空（不设上限，只受库存约束）。
	MaxQty int `json:"maxQty" form:"maxQty"`

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
	MaxOptions int `json:"maxOptions" form:"maxOptions"`
	// MinTotalQty 整单最小总件数（所有选项数量之和的下限；0 = 不设下限）。
	MinTotalQty int `json:"minTotalQty" form:"minTotalQty"`
	// MaxTotalQty 整单最大总件数（0 = 留空，不设上限）。
	MaxTotalQty int            `json:"maxTotalQty" form:"maxTotalQty"`
	Options     []BundleOption `json:"options" form:"options"`
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
	ProjectID string `form:"projectId" json:"projectId"`
	// Config 走 BindForm 自己解析（表单体里是一个 JSON 字符串）：
	// gin 的 form 映射不支持点号嵌套、encoding/json 不认字符串 —— 见 BindForm 的注释。
	Config     BundleConfig `json:"config" form:"-"`
	OperatorID string       `json:"-" form:"-"`
}

// ListBundleSKUReq 可挑选的 SKU 清单（后台配置器的下拉数据源）。
type ListBundleSKUReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
}

// ValidateBundleSelectionReq 整单校验请求。
//
// Items 是**整单的完整选择**（不是增量）：漏填的必选项在这里就是缺席，
// 校验因此是纯函数式的 —— 同样的选择永远得到同样的结论。
type ValidateBundleSelectionReq struct {
	ProductID string `json:"productId" form:"productId"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string                 `json:"projectId" form:"projectId"`
	Items     []BundleSelectItemResp `json:"items" form:"-"`
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
	ProductID string `json:"productId" form:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId" form:"projectId"`
	// Source 来源（productdto 之外只认 productenums.BundleSource* 三个值）。
	Source string `json:"source" form:"source"`
	// SourceProductID 来源商品（product / attributes 两种来源必填）。
	SourceProductID string `json:"sourceProductId" form:"sourceProductId"`
	// WarehouseID / WarehouseSKUs 仓库来源：仓 + 该仓的仓库 SKU 编码（服务端按
	// (仓库, SKU) 复核，前端提交的只是线索 —— 与新建商品「从仓库选」同一口径）。
	WarehouseID   string   `json:"warehouseId" form:"sourceWarehouse"`
	WarehouseSKUs []string `json:"warehouseSkus" form:"warehouseSku"`
	// Selections 属性组合来源的勾选（属性组 id → 属性值 id，多值）。
	//
	// 表单里没有对应键：页面按 attr:<属性组 id> 提交，由 BindForm 收。
	Selections []VariantSelectionReq `json:"selections" form:"-"`
	// ExistingVariantIDs 前端清单里已有的成员变体 id：服务端据此去重（同一变体只出现一次），
	// 前端提交的形状一律不作数 —— 它只影响「哪些行不再追加」。
	ExistingVariantIDs []string `json:"existingVariantIds" form:"existingVariantId"`
}

// BindForm 收表单里按 attr: 前缀提交的属性组合勾选（页面「解析成员」入口）。
func (r *ResolveBundleMembersReq) BindForm(vals url.Values) error {
	r.Selections = SelectionsFromForm(vals)
	return nil
}

// BundleMemberSource 成员来源快照（不是身份，见 BundleOption 的说明）。
type BundleMemberSource struct {
	Kind         string `json:"kind" form:"kind"`
	WarehouseID  string `json:"warehouseId,omitempty"`
	WarehouseSKU string `json:"warehouseSku,omitempty"`
	ExternalSKU  string `json:"externalSku,omitempty"`
}

// BindForm 表单体里 Config 是一个 JSON 字符串（htmx 侧用 JSON.stringify 生成）。
//
// 为什么不能靠反射绑定：gin 的 form 映射**不支持点号嵌套**（实测 `config.maxOptions=3`
// 绑完仍是 0），而 `encoding/json` 的 Unmarshaler 期待一个 JSON 值、不认字符串
// （实测 `config={"maxOptions":3}` 绑完也是零值）。两条路都不通，只能自己解析。
//
// Config 在 tag 上标 `form:"-"`：不让反射那条路走一半（部分字段被覆盖、部分留零值，
// 那比全零更难查）。
//
// 空串 / 未提交 → 保持零值（Config 是值类型，零值即「空配置」，与 JSON 省略同义）。
// 非空但不是合法 JSON → FieldValueError（FieldKindJSON），由出口按 400 报参数错误。
func (r *SetBundleConfigReq) BindForm(vals url.Values) error {
	raw := strings.TrimSpace(vals.Get("config"))
	if raw == "" {
		return nil
	}
	if !json.Valid([]byte(raw)) {
		return &FieldValueError{Field: "config", Kind: FieldKindJSON}
	}
	if err := json.Unmarshal([]byte(raw), &r.Config); err != nil {
		return &FieldValueError{Field: "config", Kind: FieldKindJSON}
	}
	return nil
}
