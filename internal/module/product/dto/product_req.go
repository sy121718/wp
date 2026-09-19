// product_req.go — product 模块入参（inbound 绑定用）。
package productdto

import "encoding/json"

// CreateReq 新建商品。
//
// 关联关系一律走 JSON 数组（图片 URL / 分类 id / 标签 id / 关联商品 id / 捆绑 BOM），
// 见 spec §数据结构精简原则：不为每类关系单开关联表。
type CreateReq struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name" binding:"required"`
	// Type 商品类型（迁移 238）：空 = variant（常规变体商品）；bundle = 捆绑容器。
	// 只在创建时确定 —— bundle 不生成首个变体，价格只取 DefaultPrice（容器价，必须 > 0）。
	Type        string          `json:"type"`
	Subtitle    string          `json:"subtitle"`
	Description json.RawMessage `json:"description"`
	Slug        string          `json:"slug"`
	// SKUCode 商品主体 SKU 编码（规则见 docs/14-product-sku-and-cost-model.md）。
	//
	// 语义：运营填的商品主体编码。留空则按规则派生 —— 变体商品取商品 URL 段的 ASCII 段，
	// 捆绑商品取 <商品段>_B；派生不出 ASCII 段时明确报错（ErrSkuContainerMissing），
	// 不退回随机码。填了时：变体商品原样保留、选了仓库自动加仓库码前缀（已带前缀不重复加）；
	// 捆绑商品恒以 _B 结尾（缺后缀补齐）。
	// **存量编码一律不重写** —— 本字段只作用于新建商品。
	SKUCode string `json:"sku"`
	// SKUSource 新建商品的 SKU 来源（docs/14 §1.1 的两条入口）：
	//
	//	"" / "custom" —— 自己创建（默认行为：填 SKUCode，选了仓库自动加仓码前缀）；
	//	"warehouse"  —— 从仓库选（必须同时给 WarehouseID 与 WarehouseSKU）。
	//
	// 空串按自己创建归一；非法取值明确报 ErrSKUSourceInvalid（不静默退化 —— 静默会让
	// 「从仓库选」的意图消失得无声无息）。捆绑商品不允许 warehouse。
	SKUSource string `json:"skuSource"`
	// WarehouseSKU 「从仓库选」时该仓的那条货（inventory_stocks.sku_code）。
	//
	// 服务端会带着 WarehouseID 在该仓复核它确实存在，**编码本体也取自仓库那一行** ——
	// 这条路径上 SKUCode 不被采信（服务端不信任前端，见 docs/14 §9.3）。
	WarehouseSKU string `json:"warehouseSku"`
	// ExternalSKU 这条库存行在**该仓**的外部 / 第三方编码（迁移 251；空 = 该仓用我们
	// 自己的 SKU）。从仓库选时留空即带入所选那条货的编码；自己创建 + 回填时填了才写。
	// 同一个外部编码在同一仓库内只能属于同一个商品（同商品的多个变体可以共用）。
	ExternalSKU    string   `json:"externalSku"`
	Unit           string   `json:"unit"`
	Weight         *float64 `json:"weight"`
	SEOTitle       string   `json:"seoTitle"`
	SEODescription string   `json:"seoDescription"`
	Images         []string `json:"images"`
	// ImageAlts 图集 alt 文本（issue #12）：与 Images 逐位对应，元素可为空串。
	// 单独成字段而不是把 alt 塞进 URL 字符串：alt 是作者填写的文本，参与内容翻译
	// （语境 product.imageAlts），URL 永不翻译（验收 4/5）。
	ImageAlts []string `json:"imageAlts"`
	// AttributeIDs 引用的属性组 id（issue #7）：只存引用，组与值的定义唯一一份，
	// 同一属性组可被多个商品复用。
	AttributeIDs []string `json:"attributeIds"`
	CategoryIDs  []string `json:"categoryIds"`
	// PrimaryCategoryID 主分类（issue #10）。主分类必然是附属分类之一：
	// 显式指定但它不在 CategoryIDs 里时由服务端自动纳入，不变量始终成立。
	PrimaryCategoryID string          `json:"primaryCategoryId"`
	TagIDs            []string        `json:"tagIds"`
	RelatedIDs        []string        `json:"relatedIds"`
	BundleItems       json.RawMessage `json:"bundleItems"`
	// BrandID 品牌引用（issue #10）：必须是同工程内真实存在的品牌，空串即不指定。
	BrandID      string          `json:"brandId"`
	DefaultPrice *float64        `json:"defaultPrice"`
	DefaultImage string          `json:"defaultImage"`
	Metadata     json.RawMessage `json:"metadata"`
	// WarehouseID 归属仓（issue #15）：新建商品时首个变体落在该仓；
	// 为空则兜底该工程的默认仓。归属仓的短码同时决定首个变体的 SKU 编码前缀。
	//
	// **单值形态保留兼容**：多仓口径（WarehouseIDs）落地后它仍然是合法的调用方式
	//（等价于只勾一个仓），旧的接口调用方与存量测试不必改写。两者同时给时以 WarehouseIDs 为准。
	WarehouseID string `json:"warehouseId"`
	// WarehouseIDs 本次要在哪些仓各建一行（多仓，2026-09-19 口径）。
	//
	// 三条语义（docs/14 §1.1）：
	//   · 勾了哪些仓就在哪些仓各建一行 —— 仓库侧写**裸码**（剥掉仓码前缀的那一串）；
	//   · 该仓已有同裸码的行 → **复用那一行，不新建**（幂等）；
	//   · **认领仓 = 本列表的第一个仓**（为空则默认仓），只有它决定主体 SKU 的仓码前缀。
	//
	// 空列表回落到 WarehouseID（再空则默认仓）：模板是勾选列表（同名多值），
	// 一个都没勾时浏览器不提交这个字段，服务端按「默认仓」处理。
	WarehouseIDs []string `json:"warehouseIds"`
	// Quantity 新建库存行的数量（nil = **不跟踪 = 无限**，新建行的默认口径）。
	//
	// 语义与列口径一一对应（迁移 261）：
	//   · nil  → track_quantity = false，数量恒 0（运营不填就是无限）；
	//   · 非 nil → track_quantity = true 并写入该数量，**0 是合法值**（明确没货），
	//     与 nil 严格区分 —— 这正是 CHECK (track_quantity OR quantity = 0) 要表达的事。
	// 勾了几个仓就写几行，同一个数量应用到本次新建的每一行。
	Quantity *int `json:"quantity"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json/form 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-" form:"-"`
}

// UpdateReq 修改商品（含 slug 改名；变体单独接口）。
type UpdateReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string  `json:"projectId"`
	Name      *string `json:"name"`
	// Type 商品类型：只在创建时确定。传入且与现值不同即拒绝（ErrProductTypeImmutable）——
	// 变体 ↔ 捆绑的切换涉及「有没有自己的 SKU / 价格放在哪」，不是一次字段更新能表达的。
	Type        *string         `json:"type"`
	Subtitle    *string         `json:"subtitle"`
	Description json.RawMessage `json:"description"`
	Slug        *string         `json:"slug"`
	// SKUCode 商品主体 SKU 编码（显式修改才动 products.sku_code）：nil 表示本次不改 ——
	// 存量编码不被隐式重写；给了空串按 ErrContainerSkuInvalid 拒绝（静默忽略会让人以为改成功了）。
	// 捆绑商品缺 _B 后缀时补齐；这里不补仓库码前缀（仓码前缀是创建时按归属仓确定的，
	// 编辑路径没有仓上下文，要换前缀走新建商品）。
	SKUCode        *string  `json:"sku"`
	Status         *string  `json:"status"`
	Unit           *string  `json:"unit"`
	Weight         *float64 `json:"weight"`
	SEOTitle       *string  `json:"seoTitle"`
	SEODescription *string  `json:"seoDescription"`
	Images         []string `json:"images"`
	// ImageAlts 图集 alt 文本（issue #12）；nil 表示本次不改，空数组表示全部清空
	// （与 Images 的「整体替换」语义一致）。
	ImageAlts []string `json:"imageAlts"`
	// AttributeIDs 引用的属性组 id（issue #7）；nil 表示本次不改引用，
	// 空数组表示解绑全部（与 CategoryIDs / TagIDs 同为「整体替换」语义）。
	AttributeIDs []string `json:"attributeIds"`
	CategoryIDs  []string `json:"categoryIds"`
	// PrimaryCategoryID 主分类（issue #10）：nil 表示本次不改，指向空串表示解绑。
	// 附属分类被整体替换且新列表里没有原主分类时，主分类自动解绑（不变量维护）。
	PrimaryCategoryID *string         `json:"primaryCategoryId"`
	TagIDs            []string        `json:"tagIds"`
	RelatedIDs        []string        `json:"relatedIds"`
	BundleItems       json.RawMessage `json:"bundleItems"`
	BrandID           *string         `json:"brandId"`
	DefaultPrice      *float64        `json:"defaultPrice"`
	DefaultImage      *string         `json:"defaultImage"`
	Metadata          json.RawMessage `json:"metadata"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json/form 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-" form:"-"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	ID        string `form:"id" binding:"required"`
}

// ListReq 商品列表（分页 + 可选过滤）。
type ListReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
	Status    string `form:"status"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

// DeleteReq 删除商品（连带其变体）。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json/form 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-" form:"-"`
}

// CreateVariantReq 为商品新增一个变体。
//
// 未填字段由 service 逐字段判空后继承商品级默认值（空值以 NULL 判定，0 与 false 视为已填）。
type CreateVariantReq struct {
	ProductID string `json:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID    string          `json:"projectId"`
	SKUCode      string          `json:"skuCode"`
	Barcode      string          `json:"barcode"`
	Price        *float64        `json:"price"`
	ComparePrice *float64        `json:"comparePrice"`
	CostPrice    *float64        `json:"costPrice"`
	Image        string          `json:"image"`
	OptionValues json.RawMessage `json:"optionValues"`
	Enabled      *bool           `json:"enabled"`
	Sort         int             `json:"sort"`
	// WarehouseID 归属仓（issue #15）：不选则兜底该工程的默认仓；
	// 无论选没选，该 SKU 都会在归属仓生成一条库存记录（初始 0）。
	WarehouseID string `json:"warehouseId"`
	// Quantity 该变体库存行的数量（nil = 不跟踪 = 无限，与商品创建同一条口径）。
	// 该变体的库存行走「新增路径」时才写数量：已存在的行不覆盖（覆盖走库存页的显式入口）。
	Quantity *int `json:"quantity"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json/form 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-" form:"-"`
}

// UpdateVariantReq 修改变体（编辑路径不做默认值填充）。
type UpdateVariantReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID    string          `json:"projectId"`
	SKUCode      *string         `json:"skuCode"`
	Barcode      *string         `json:"barcode"`
	Price        *float64        `json:"price"`
	ComparePrice *float64        `json:"comparePrice"`
	CostPrice    *float64        `json:"costPrice"`
	Image        *string         `json:"image"`
	OptionValues json.RawMessage `json:"optionValues"`
	Enabled      *bool           `json:"enabled"`
	Sort         *int            `json:"sort"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json/form 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-" form:"-"`
}

// DeleteVariantReq 删除变体。
type DeleteVariantReq struct {
	ID string `json:"id" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID string `json:"projectId"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json/form 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-" form:"-"`
}

// VariantSelectionReq 一组被勾选的属性值（issue #8）。
//
// AttributeID 必须是该商品已引用、且 IsVariation=true 的属性组；ValueIDs 是组内
// 被勾选的属性值 id（只允许组内**启用**的值）。重复的值 id 会在生成时去重，
// 因此「重复勾选」不会产生重复变体。
type VariantSelectionReq struct {
	AttributeID string   `json:"attributeId"`
	ValueIDs    []string `json:"valueIds"`
}

// GenerateVariantsReq 按勾选的属性值生成全部变体组合（issue #8）。
//
// 三条调用路径共用同一份语义（后台表单 / 批量生成 / 导入接口）：
//   - Selections 里出现的属性组按勾选的值参与组合（顺序固定为组内定义顺序，
//     重复勾选同一个值会被去重）；
//   - Selections 里没出现的属性组取**全部启用值**（勾了颜色不勾尺码 = 颜色按勾选的来、
//     尺码取全部）—— 生成的组合恒覆盖全部参与变体的维度，不会产出只有部分维度的
//     「半截组合」；
//   - Selections 为空 = 无表单路径：全部参与变体的属性组 × 全部启用值。
//
// 保护性上限：参与维度与组合总数分别有上限，超过时整体拒绝，一条变体都不写。
// 已存在的规格组合一律跳过（幂等），因此重复提交不会产生重复变体。
type GenerateVariantsReq struct {
	ProductID string `json:"productId" binding:"required"`
	// ProjectID 是工程隔离（DB-009）的作用域来源；为空时由 service 走唯一工程兜底。
	ProjectID  string                `json:"projectId"`
	Selections []VariantSelectionReq `json:"selections"`
	// WarehouseID 归属仓（issue #15）：本批新建的变体都落在该仓（不选则默认仓），
	// 并在该仓为每个新变体生成初始 0 的库存记录。
	WarehouseID string `json:"warehouseId"`
	// OperatorID 操作人（issue #19）：由 inbound 从会话覆盖写入，客户端传入的值被忽略
	// （json/form 标签为 "-"，不可由外部指定）。变更记录记「谁改的」，
	// 统一取会话里的登录名；缺失时为空串（留痕字段允许为空）。
	OperatorID string `json:"-" form:"-"`
}
