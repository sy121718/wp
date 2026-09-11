// Package productenums product 模块响应消息。
package productenums

// 响应消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"
	// MsgVariantGenerateSuccess 变体组合生成成功（issue #8）。
	MsgVariantGenerateSuccess = "MsgVariantGenerateSuccess"

	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrInvalidType  = "ErrInvalidType"  // 实体类型不合法（构建期字段解析）
	ErrInvalidField = "ErrInvalidField" // 字段不在商品字段白名单内
	ErrNotFound     = "ErrNotFound"     // 商品或变体不存在
	ErrSlugTaken    = "ErrSlugTaken"    // 同工程下 slug 已占用
	ErrSkuTaken     = "ErrSkuTaken"     // 同商品下 SKU 编码已占用
	ErrNameRequired = "ErrNameRequired" // 商品名称必填

	// —— 属性组（issue #7）——
	ErrAttrNameRequired      = "ErrAttrNameRequired"      // 属性组名称必填
	ErrAttrKeyRequired       = "ErrAttrKeyRequired"       // 属性组标识不能改为空
	ErrAttrKeyTaken          = "ErrAttrKeyTaken"          // 同工程下属性组标识已占用
	ErrAttrNotFound          = "ErrAttrNotFound"          // 属性组不存在
	ErrAttrProjectMismatch   = "ErrAttrProjectMismatch"   // 属性组不属于该商品所在工程
	ErrAttrInUse             = "ErrAttrInUse"             // 属性组已被商品引用，不能删除
	ErrAttrValueLabelMissing = "ErrAttrValueLabelMissing" // 属性值名称必填

	// —— 分类（issue #10）——
	ErrCategoryNameRequired    = "ErrCategoryNameRequired"    // 分类名称必填
	ErrCategoryNotFound        = "ErrCategoryNotFound"        // 分类不存在
	ErrCategorySlugTaken       = "ErrCategorySlugTaken"       // 同工程下分类 slug 已占用
	ErrCategoryParentMismatch  = "ErrCategoryParentMismatch"  // 父分类不属于该分类所在工程
	ErrCategoryCycle           = "ErrCategoryCycle"           // 不能把分类挂到自身或自己的后代下
	ErrCategoryHasChildren     = "ErrCategoryHasChildren"     // 分类仍有子级，不能删除
	ErrCategoryInUse           = "ErrCategoryInUse"           // 分类已被商品引用，不能删除
	ErrCategoryProjectMismatch = "ErrCategoryProjectMismatch" // 分类不属于该商品所在工程

	// —— 品牌（issue #10）——
	ErrBrandNameRequired    = "ErrBrandNameRequired"    // 品牌名称必填
	ErrBrandNotFound        = "ErrBrandNotFound"        // 品牌不存在
	ErrBrandSlugTaken       = "ErrBrandSlugTaken"       // 同工程下品牌 slug 已占用
	ErrBrandInUse           = "ErrBrandInUse"           // 品牌已被商品引用，不能删除
	ErrBrandProjectMismatch = "ErrBrandProjectMismatch" // 品牌不属于该商品所在工程

	// —— 标签（issue #11）——
	ErrTagNameRequired      = "ErrTagNameRequired"      // 标签名称必填
	ErrTagNotFound          = "ErrTagNotFound"          // 标签不存在
	ErrTagSlugTaken         = "ErrTagSlugTaken"         // 同工程下标签 slug 已占用
	ErrTagProjectMismatch   = "ErrTagProjectMismatch"   // 标签不属于该商品所在工程
	ErrTagKindInvalid       = "ErrTagKindInvalid"       // 标签类型不是 manual / rule
	ErrTagRuleNotAllowed    = "ErrTagRuleNotAllowed"    // 手工标签不能带自动规则
	ErrTagRuleTypeInvalid   = "ErrTagRuleTypeInvalid"   // 规则类型不是内置类型（不接受自由表达式）
	ErrTagRuleParamsInvalid = "ErrTagRuleParamsInvalid" // 规则参数不合法（键/类型/取值范围）
	ErrTagNotManual         = "ErrTagNotManual"         // 自动标签的归属由重算维护，不能手工挂载

	// —— 集合源（issue #9）——
	ErrCollectionSourceInvalid = "ErrCollectionSourceInvalid" // 集合源标识不合法（不是本模块实现的源）
	ErrCollectionFilterInvalid = "ErrCollectionFilterInvalid" // 过滤维度不在集合源白名单内

	// —— 变体组合生成（issue #8）——
	// 上限类错误的详细数值由 service 拼进消息（如「：7 个组合超过上限 200」），
	// 这里只保留稳定的错误标识，避免同一上限在多处各写一份数字。
	ErrVariationNoDimension      = "ErrVariationNoDimension"      // 没有可参与变体的属性值
	ErrVariationDimensionLimit   = "ErrVariationDimensionLimit"   // 参与变体的属性维度超过上限
	ErrVariationCountLimit       = "ErrVariationCountLimit"       // 变体组合数超过上限
	ErrVariationAttributeInvalid = "ErrVariationAttributeInvalid" // 勾选的属性组未参与该商品的变体
	ErrVariationValueInvalid     = "ErrVariationValueInvalid"     // 勾选的属性值不属于该属性组或已停用
	ErrVariationSelectionEmpty   = "ErrVariationSelectionEmpty"   // 未勾选任何属性值
)

// 商品状态。
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"
)

// 标签类型（issue #11）：manual 手工挂载 / rule 按内置规则自动维护归属。
const (
	TagKindManual = "manual"
	TagKindRule   = "rule"
)

// 内置规则类型（issue #11）：只接受这几个类型 + 各自的白名单参数，
// 不接受自由表达式（SQL / 脚本 / 表达式串一律拒绝）。
const (
	// TagRuleNewArrival 新品：上架 N 天内（参数 days）。
	TagRuleNewArrival = "new_arrival"
	// TagRulePriceRange 价格区间：存在启用变体价格落在区间内（参数 minPrice / maxPrice，至少一个）。
	TagRulePriceRange = "price_range"
	// TagRuleOnSale 促销：存在启用变体有划线价（对比价高于售价）；无参数。
	TagRuleOnSale = "on_sale"
)
