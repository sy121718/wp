// product_row_view.go — 商品行（列表 / 详情 / 编辑三页共用的成品行）。
//
// 行装配的家在 service（service/product_row.go）：取词、评分、分类/品牌/标签勾选态、库存三态
// 都在那边算完，控制器不再拼 []map[string]any / []gin.H —— 一屏的取数编排与视图装配都在
// service（见 internal/module/CLAUDE.md §inbound/http）。
//
// **导出字段名就是模板契约**：products.html / product_detail.html / product_edit.html 里的
// p.X 与 .Product.X 必须在这里有同名导出字段，双向对账由
// inbound/http/product_row_template_test.go 用反射 + 模板源码钉住（字段被改名或删掉 =
// 渲染期 500 且 htmx 不 swap，点一下没反应，只有对账测试会当场变红）。
//
// 三条与「形状」有关的约定，改这里之前先读：
//
//   - **嵌套值一律保持 map[string]any / []map[string]any**，不换成子结构体。模板对可选项用的是
//     isset(o.LabelKey)（「键在不在」），而 Jet 对 struct 的 string/int/bool 字段 isset 恒为真
//     —— 换成结构体后「空项给词条、数据项只给 Label」这条约定会被静默打破，数据行会走取词
//     分支渲染成空白（TestTaxonomyAdminPages 实测过）。
//   - **只在部分路径出现的整块数据用可 nil 的 map**：Bundle 只在 type=bundle 时给，
//     模板的 isset(p.Bundle) 靠 nil map 判假 —— 不能给空 map（isset 会变真，区块照渲染、
//     表格全空），也不能用非 map 类型。
//   - Stock 恒非空（三态口径由 service 的库存单元格给出），模板的 isset(p.Stock) 因此恒为真，
//     与「键一直在」的旧行为等价。
package productdto

// ProductRow 商品行的渲染视图（列表页的一行 / 详情页 / 编辑页的同一个商品）。
//
// 汇总段（VariantCount / PriceMin / PriceMax / HasRating / RatingAvg / RatingCount /
// CategoryCell / CategoryOthers / BrandName / TagLabel）只回答「有哪些商品」，列表页只用这一段；
// 明细段（Variants / Ratings / CategoryChecks / TagChecks / AutoTags / VariationAttributes …）
// 是某个商品的子资源，详情页与编辑页用。两页共用**同一份组装**：同一个值在两处各算一遍，
// 分叉时没有任何东西会报错。
type ProductRow struct {
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
	// 已归一为存储 URL（upload.StorageURL）。**必须与详情响应一起赋值**：保存路径
	// （ProductsUpdate）会读这个隐藏域，所以「这里空着」与「保存时清空」是同一件事。
	DefaultImage string

	PriceMin string
	PriceMax string

	VariantCount int
	// Variants 变体清单行（docs/14 §8）：种子是库里已有的变体，行上带可编辑的 SKU 与
	// 规范化后的 option_values（保存时原样回传）。
	Variants []map[string]any

	// Attributes 引用的属性组（同一属性组可被多个商品共用，这里只展示引用与属性值）；
	// VariationAttributes 只列参与变体的组（组合生成面板用）。
	Attributes          []*AttributeResp
	VariationAttributes []*AttributeResp

	// 分类与品牌：勾选态 / 选中态都由 service 算好，模板只做展示。
	CategoryChecks []map[string]any
	// CategoryCell / CategoryOthers 是列表列要的「一个值 + 其余数量」：
	// 一列一个概念，单元格放值，不把「挂载 N 个分类 · 主分类 X」拼成一句话。
	CategoryCell   string
	CategoryOthers int
	// PrimaryOptions / BrandOptions 是「含空项的下拉」的选项事实：空项带词条 key + 中文兜底，
	// 数据项只给名字 —— 取词在模板（见 service 的 productPickOptions）。
	PrimaryOptions []map[string]any
	BrandOptions   []map[string]any
	BrandName      string

	// 标签：TagChecks 手工标签勾选挂载，AutoTags 是只读的自动标签（归属由规则重算维护）；
	// TagLabel 是列表列要的单个值（手工 + 自动合并的标签名）。
	TagChecks []map[string]any
	AutoTags  []map[string]any
	TagLabel  string

	// 评分：明细 + 投影值。没有评分时 HasRating=false —— 空态与「评分 0」是两回事。
	Ratings     []map[string]any
	HasRating   bool
	RatingAvg   string
	RatingCount int

	// Stock 库存列（三态 + 分仓明细）；恒非空。
	Stock map[string]any

	// Bundle 捆绑构成面板（只在 type=bundle 时给，nil = 模板整块跳过）。
	Bundle map[string]any
}
