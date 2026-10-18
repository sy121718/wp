// product_resp.go — product 模块出参（service 返回）。
package productdto

import json "go_wp/pkg/jsonx"

// ProductResp 商品详情（含变体列表）。
type ProductResp struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"projectId"`
	Name        string          `json:"name"`
	Subtitle    string          `json:"subtitle"`
	Description json.RawMessage `json:"description"`
	Slug        string          `json:"slug"`
	// SKUCode 商品主体 SKU（products.sku_code，迁移 246）：变体商品的「容器主体」，
	// 捆绑商品的自定义主体（恒 _B 结尾）。唯一性范围是**本工程**（部分唯一索引
	// uq_products_project_sku_code），不是全局；变体 SKU 以它为主体拼接。
	SKUCode string `json:"skuCode"`
	Status  string `json:"status"`
	// Type 商品类型（迁移 238）：variant | bundle。
	Type string `json:"type"`
	// PublishedAt 上架时间（issue #11，RFC3339；未上架为空串）：
	// 自动标签的「新品」规则以它为判定基准。
	PublishedAt    string   `json:"publishedAt"`
	Sort           int      `json:"sort"`
	Unit           string   `json:"unit"`
	Weight         *float64 `json:"weight"`
	SEOTitle       string   `json:"seoTitle"`
	SEODescription string   `json:"seoDescription"`
	Images         []string `json:"images"`
	// ImageAlts 图集 alt 文本（issue #12）：与 Images 逐位对应，元素可为空串。
	ImageAlts []string `json:"imageAlts"`
	// AttributeIDs 商品引用的属性组 id（issue #7）。
	AttributeIDs []string `json:"attributeIds"`
	CategoryIDs  []string `json:"categoryIds"`
	// PrimaryCategoryID 主分类（issue #10）：空串表示未指定；指定时必然同时出现在
	// CategoryIDs 里（服务端维持的不变量）。
	PrimaryCategoryID string          `json:"primaryCategoryId"`
	TagIDs            []string        `json:"tagIds"`
	RelatedIDs        []string        `json:"relatedIds"`
	BundleItems       json.RawMessage `json:"bundleItems"`
	BrandID           string          `json:"brandId"`
	DefaultPrice      *float64        `json:"defaultPrice"`
	DefaultImage      string          `json:"defaultImage"`
	Metadata          json.RawMessage `json:"metadata"`
	// PriceMin / PriceMax 只读价格区间：变体商品从变体派生；**捆绑容器取容器价**
	//（bundle 没有自己的 SKU，成员价不参与计价，见 applyPriceRange）。
	PriceMin float64 `json:"priceMin"`
	PriceMax float64 `json:"priceMax"`
	// VariantCount 变体数量（单变体商品在前台不显示规格选择器）。
	VariantCount int            `json:"variantCount"`
	Variants     []*VariantResp `json:"variants,omitempty"`
	// Attributes 商品引用的属性组（含值）—— 同一属性组可被多个商品复用，
	// 这里返回的是共享定义本身，不是副本（issue #7）。
	Attributes []*AttributeResp `json:"attributes,omitempty"`
	// Categories 商品挂载的分类（issue #12：后台翻译页要展示分类名与描述，
	// 故详情接口一并返回引用的分类实体，而不是只给 id 列表）。
	Categories []*CategoryResp `json:"categories,omitempty"`
	// Brand 商品指定的品牌（同上，未指定为 nil）。
	Brand *BrandResp `json:"brand,omitempty"`
	// Tags 商品挂载/命中的标签（同上）。
	Tags      []*TagResp `json:"tags,omitempty"`
	CreatedAt string     `json:"createdAt"`
	UpdatedAt string     `json:"updatedAt"`

	// —— 库存聚合（商品列表「库存」列，docs/14 §1.4，2026-09-19 口径）——
	//
	// 真源只有 inventory_stocks（商品侧不留任何副本），这三项是**查询期聚合的虚拟字段**：
	//
	//	StockState — infinite（任一仓不跟踪）｜ tracked（全部跟踪）｜ none（任何仓都没有行）；
	//	StockTotal — 仅 tracked 时有意义，等于各仓各行的数量之和；**0 显示 0**（≠ 未入库）；
	//	StockWarehouses — 分仓明细（悬浮 / details 展开用），一行一个仓，三态同样区分。
	//
	// 死线：混合状态**绝不求和**。任一仓不跟踪就整体显示「无限」——
	// 求和等于把无限当 0，页面会显示成「有货」，而实际是「卖不完」。
	StockState string `json:"stockState"`
	StockTotal int    `json:"stockTotal"`
	// StockWarehouses 分仓明细：只列**产生过库存行**的仓（没有任何行的仓在页面上显示「未入库」，
	// 由页面按工程仓库清单补齐 —— 服务端不凭空造行，见 fillProductStock 的注释）。
	StockWarehouses []*ProductWarehouseStockResp `json:"stockWarehouses,omitempty"`
}

// ProductWarehouseStockResp 商品在某个仓的库存明细（列表「库存」列的折叠内容）。
//
// 一行一个仓：该商品在这个仓的全部库存行（多规格 = 多行）已按三态口径归并 ——
// State 与 Quantity 是这个仓的结论，不是某一行原样透出。
type ProductWarehouseStockResp struct {
	WarehouseID   string `json:"warehouseId"`
	WarehouseCode string `json:"warehouseCode"`
	WarehouseName string `json:"warehouseName"`
	// SKUCode 仓库侧**裸码**（不带仓码前缀）：多规格时取该仓第一条的编码，
	// 仅作对照展示 —— 真正的身份是 variantId（SKU 串只用于展示与对账）。
	SKUCode string `json:"skuCode"`
	// State 该仓的三态：infinite（该仓任一行不跟踪）｜ tracked（全部跟踪）｜ none（该仓没有行）。
	State string `json:"state"`
	// TrackQuantity / Quantity 仅 State = tracked 时有意义（Quantity 为该仓各行之和）。
	TrackQuantity bool `json:"trackQuantity"`
	Quantity      int  `json:"quantity"`
	// CostPrice 该仓第一条登记过成本的行（(仓库, SKU) 的当前成本，NULL = 未核算）。
	// 多规格同仓多行时只展示其中之一：成本是 (仓库, SKU) 维度的事实，不是商品的属性。
	CostPrice *float64 `json:"costPrice"`
}

// VariantResp 变体。
type VariantResp struct {
	ID           string          `json:"id"`
	ProductID    string          `json:"productId"`
	SKUCode      string          `json:"skuCode"`
	Barcode      string          `json:"barcode"`
	Price        float64         `json:"price"`
	ComparePrice *float64        `json:"comparePrice"`
	CostPrice    *float64        `json:"costPrice"`
	Image        string          `json:"image"`
	OptionValues json.RawMessage `json:"optionValues"`
	Enabled      bool            `json:"enabled"`
	Sort         int             `json:"sort"`
	StockTotal   int             `json:"stockTotal"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

// GenerateVariantsResp 变体组合生成结果（issue #8）。
type GenerateVariantsResp struct {
	ProductID string `json:"productId"`
	// Total 本次笛卡尔积的组合总数（去重前）。
	Total int `json:"total"`
	// Created 新建的变体数。
	Created int `json:"created"`
	// Adopted 由商品的「无规格占位变体」就地承接的组合数（0 或 1）。
	Adopted int `json:"adopted"`
	// Skipped 已存在（或同一次请求内重复）而跳过的组合数。
	Skipped int `json:"skipped"`
	// Variants 生成后该商品的全部变体。
	Variants []*VariantResp `json:"variants"`
}

// ListResp 商品列表项（不含变体明细与 metadata —— metadata 默认查询不取）。
type ListResp struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Status       string   `json:"status"`
	Images       []string `json:"images"`
	PriceMin     float64  `json:"priceMin"`
	PriceMax     float64  `json:"priceMax"`
	VariantCount int      `json:"variantCount"`
	UpdatedAt    string   `json:"updatedAt"`
}

// ProductRowResp 商品行的渲染视图（列表页的一行 / 详情页 / 编辑页的同一个商品）。
//
// 汇总段（VariantCount / PriceMin / PriceMax / HasRating / RatingAvg / RatingCount /
// CategoryCell / CategoryOthers / BrandName / TagLabel）只回答「有哪些商品」，列表页只用这一段；
// 明细段（Variants / Ratings / CategoryChecks / TagChecks / AutoTags / VariationAttributes …）
// 是某个商品的子资源，详情页与编辑页用。两页共用**同一份组装**：同一个值在两处各算一遍，
// 分叉时没有任何东西会报错。
type ProductRowResp struct {
	ID   string
	Name string
	Slug string
	// SKUCode 主体 SKU（products.sku_code）：详情页基本信息区只读展示 + 一句话说明唯一性范围。
	SKUCode string
	Status  string

	// Subtitle / Unit / DescriptionHTML 只有编辑表单读（列表与详情页不需要，
	// 所以它们不进「汇总段」那批共用取值）。
	Subtitle string
	Unit     string
	// DescriptionHTML 商品描述在库里是 {"html": "..."}（jsonb 对象），富文本字段要的是裸 HTML。
	DescriptionHTML string
	// DefaultImage 主图（编辑页的单图媒体控件）：与 ProductResp.DefaultImage 同源，
	// 已归一为存储 URL（upload.StorageURL）。**必须与详情响应一起赋值**：编辑页保存时
	// 基座会把表单里的隐藏域整份发成 UpdateReq.DefaultImage，所以「这里空着」与
	// 「保存时清空」是同一件事。
	DefaultImage string

	PriceMin string
	PriceMax string

	VariantCount int
	// Variants 变体清单行（docs/14 §8）：种子是库里已有的变体，行上带可编辑的 SKU 与
	// 规范化后的 option_values（保存时原样回传）。
	Variants []VariantListRowResp

	// Attributes 引用的属性组（同一属性组可被多个商品共用，这里只展示引用与属性值）；
	// VariationAttributes 只列参与变体的组（组合生成面板用）。
	Attributes          []*AttributeResp
	VariationAttributes []*AttributeResp

	// 分类与品牌：勾选态 / 选中态都由 service 算好，模板只做展示。
	CategoryChecks []CategoryCheckRowResp
	// CategoryCell / CategoryOthers 是列表列要的「一个值 + 其余数量」：
	// 一列一个概念，单元格放值，不把「挂载 N 个分类 · 主分类 X」拼成一句话。
	CategoryCell   string
	CategoryOthers int
	// PrimaryOptions / BrandOptions 是「含空项的下拉」的选项事实：空项带词条 key + 中文兜底，
	// 数据项只给名字 —— 取词在模板（见 service 的 productPickOptions）。
	PrimaryOptions []PickOptionResp
	BrandOptions   []PickOptionResp
	BrandName      string

	// 标签：TagChecks 手工标签勾选挂载，AutoTags 是只读的自动标签（归属由规则重算维护）；
	// TagNames 是列表列要的标签名（手工 + 自动合并，只给事实，不拼接、不计数）。
	TagChecks []TagCheckRowResp
	AutoTags  []AutoTagRowResp
	TagNames  []string

	// 评分：明细 + 投影值。没有评分时 HasRating=false —— 空态与「评分 0」是两回事。
	Ratings     []RatingRowResp
	HasRating   bool
	RatingAvg   string
	RatingCount int

	// Stock 库存列（三态 + 分仓明细）；nil = 这一行不带库存（模板整格跳过）。
	Stock *StockCellResp

	// Bundle 捆绑构成面板（只在 type=bundle 时给，nil = 模板整块跳过）。
	Bundle *BundlePanelResp
}

// 行元素一律是结构体，只有**片段/抽屉的数据根**保持 map —— 后者是 Jet 的取值方式决定的：
// 模板用 .["t"] 取词，索引语法只对 map 生效，struct 根会渲染失败。行元素没有这个约束
// （模板一律用 .Field 读），结构体换来的是编译期保护与「字段改名模板没跟」的编译失败，
// 而不是渲染期才暴露的空值。

// VariantListRowResp 变体清单表的一行。
//
// OptionValues 是**规范化后的 JSON 文本**（空 / null 归一为 {}）：它同时是行上的 data 属性值
// 与预览去重的入参，前端 JSON.parse 所以必须是合法 JSON；Spec 是服务端拼好的规格可读文本。
type VariantListRowResp struct {
	ID           string
	SKUCode      string
	Spec         string
	OptionValues string
	Price        string
	ComparePrice string
	CostPrice    string
	Enabled      bool
	StockTotal   int
}

// CategoryCheckRowResp 分类勾选框的一行（勾选态由服务端算好，模板不做集合运算）。
type CategoryCheckRowResp struct {
	ID      string
	Label   string
	Checked bool
}

// TagCheckRowResp 手工标签勾选框的一行。
type TagCheckRowResp struct {
	ID      string
	Name    string
	Checked bool
}

// AutoTagRowResp 自动标签（只读展示：归属由规则重算维护，手工改会被下一次重算覆盖）。
type AutoTagRowResp struct {
	ID        string
	Name      string
	RuleLabel string
}

// PickOptionResp 下拉选项的一行（「含空项的下拉」共用这一份形状）。
//
// 两条取值链只在**空项**上分叉：空项的文案是事实（Key + 中文兜底，渲染期按请求语言取词），
// 数据项是数据（分类名 / 品牌名 / 商品名，翻不了）。用显式 IsNone 区分，而不是「LabelKey
// 这个键在不在」—— 后者让「缺键」与「空串」同形，数据行误给一个空串就会被当成空项走取词
// 分支，渲染出一排空白 option（不报错，也没人发现）。
type PickOptionResp struct {
	Value         string
	Label         string
	LabelKey      string
	LabelFallback string
	Selected      bool
	IsNone        bool
}

// RatingRowResp 评分明细的一行。
type RatingRowResp struct {
	ID        string
	Score     string
	Source    string
	CreatedAt string
}

// StockCellResp 库存列（三态 + 分仓明细）。
//
// 三态口径（docs/14 §1.4）：任一仓不跟踪 → infinite（**绝不求和**，求和等于把无限当 0）；
// 全部跟踪 → 各仓数量之和；一个仓都没有这一行 → none（未入库）。
type StockCellResp struct {
	State string
	Total int
	// Rows 按**工程仓库清单**逐仓列出 —— 「某仓没有库存行」同样是一条信息，
	// 只列有行的仓会让运营以为这个商品只在那几个仓。
	Rows []StockRowResp
}

// StockRowResp 分仓明细的一行。
type StockRowResp struct {
	WarehouseID    string
	WarehouseLabel string
	State          string
	Quantity       int
}
