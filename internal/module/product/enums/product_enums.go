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
	// ErrBulkRejected 批量操作里有条目不能处理：**整批不生效**（明细在错误的 data.items 里，
	// 逐条给 id / 名称 / 原因 key）。
	ErrBulkRejected    = "ErrBulkRejected"
	ErrInvalidType     = "ErrInvalidType"     // 实体类型不合法（构建期字段解析）
	ErrInvalidField    = "ErrInvalidField"    // 字段不在商品字段白名单内
	ErrNotFound        = "ErrNotFound"        // 商品或变体不存在
	ErrSlugTaken       = "ErrSlugTaken"       // 同工程下 slug 已占用
	ErrSkuTaken        = "ErrSkuTaken"        // 同商品下 SKU 编码已占用
	ErrVariantHasStock = "ErrVariantHasStock" // 变体仍有库存，不能删除（请改为停用）
	ErrNameRequired    = "ErrNameRequired"    // 商品名称必填
)

// 评论（BIZ-5）：商品评论的差异化规则由本模块实现，见 service/product_comment_policy.go。
const (
	// ErrCommentPurchaseRequired 发表商品评论前必须先购买过该商品。
	//
	// 它是**消费方**（comment 模块）要展示的拒绝理由：值即 i18n key，经
	// commentcontract.PolicyDenial.Message 传出去，中文兜底由 policy 实现随
	// Fallback 一起给（comment 侧不认识本模块的词条，只能按 key + 兜底翻译）。
	// 词条真源见 public/migrations/468_comment_purchase_i18n.sql。
	ErrCommentPurchaseRequired = "product.err.commentPurchaseRequired"
)

// 构建期上下文缺失（DB-009 第四批）。单独一块，避免把上面那批常量名的对齐列宽一起改掉。
const (
	// ErrMissingProjectContext 构建期实体字段源拿不到工程上下文（core.WithBuildProjectID 未注入）。
	//
	// 它是「调用链漏了注入」的显式信号，不能退化成裸读：裸读在非超级角色下静默 0 行，
	// 表现为「实体不存在 / 产物区块缺失」，而真实原因是上游少传了一个工程 id。
	ErrMissingProjectContext = "ErrMissingProjectContext"
)

const (
	// —— 相关商品（products.related_ids，审计 DB-03 / PROD-01）——
	// 相关商品是**同表自引用**：分类 / 标签 / 属性三条引用面都有「存在 + 同工程」校验，
	// 只有它此前原样落库 —— 任意 uuid（别的工程、已被删除、甚至自己）都能写进去。
	// 只用一个 key：三类问题（不存在 / 跨工程 / 指向自己）在同一个 tail 里**一次列全**
	// （逐条报会逼运营「改一条、提交一次」，而它们本就是同一次提交里的同一批引用）。
	ErrRelatedInvalid = "ErrRelatedInvalid" // 相关商品引用不合法（不存在 / 跨工程 / 指向自己）

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
	ErrTagCrossProject      = "ErrTagCrossProject"      // 标签仍被其它工程的商品引用，不能删除
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

	// —— 变体清单：预览—保存模型（docs/14 §8，2026-09-19 用户拍板，迁移 253 配套）——
	//
	// 清单里的「生成 / 删除」都不落库，只有「保存」才是权威动作；因此服务端对清单行
	// 承担与「直接写库」同等的校验责任（不信任前端提交的形状）。
	//
	//	ErrVariantSKUEmpty      —— 行上的 SKU 为空（或规范化后为空）且系统生成不出编码；
	//	ErrVariantOptionsInvalid —— 新增行的规格组合不合法（空组合 / 组不属于商品 / 取值越界）。
	ErrVariantSKUEmpty       = "ErrVariantSKUEmpty"       // 变体的 SKU 编码不能为空
	ErrVariantOptionsInvalid = "ErrVariantOptionsInvalid" // 变体的规格组合不合法

	// ErrVariantStockDuplicated 仓库明细里同一仓同一仓库 SKU 出现多次（B8）。
	//
	// 为什么单独一条而不是复用 ErrInvalidParam：这是运营**能改**的输入问题（合并成一行），
	// 归口文案「参数无效」会让他翻遍整张表单去找哪一格错了。
	ErrVariantStockDuplicated = "ErrVariantStockDuplicated"

	// 保存时被跳过的一行（不整批失败，逐条回带原因；值即 i18n key）。
	// 与定价工具的 PricingSkip* 同一形态：跳过是「这一行没做」，不是「整批失败」。
	VariantSkipHasStock       = "VariantSkipHasStock"       // 该变体仍有非零库存
	VariantSkipDuplicated     = "VariantSkipDuplicated"     // 清单里重复的规格组合（只保留第一行）
	VariantSkipVariantMissing = "VariantSkipVariantMissing" // 清单引用的既有变体已不存在（并发删除）

	// 删除守卫的另一个引用面（迁移 259 配套）：有过任何库存流水 —— 订单一旦建单就必然
	// 产生扣减流水，所以「有流水」等价于「被订单用过」（历史单据按 variant_id 追溯，
	// 不允许硬删）。与非零库存互补：卖出后补货清零的变体库存为 0 却仍被用过，两个守卫都要。
	VariantSkipHasMovement = "VariantSkipHasMovement" // 该变体有过库存流水（被订单用过）

	// —— 商品主体 SKU 编码（2026-09-19 评审第四轮，迁移 248 配套）——
	// 两条都是「可行动的」错误：运营要么显式填一个编码，要么把 URL 段改成 ASCII。
	ErrSkuContainerMissing = "ErrSkuContainerMissing" // 商品 URL 段不含 ASCII 字符，派生不出主体 SKU 编码
	ErrContainerSkuInvalid = "ErrContainerSkuInvalid" // 显式填的主体 SKU 编码不合法（空串）

	// ErrContainerSKUTaken 主体 SKU 在本工程已被**其它商品**占用（主体编码在工程内唯一）。
	//
	// 与 ErrSkuTaken 的分工是**唯一性范围**，不是措辞差异：
	//   · ErrSkuTaken        —— 变体 SKU 在**同一商品内**重复（UNIQUE (product_id, sku_code)）；
	//   · ErrContainerSKUTaken —— 主体 SKU 在**整个工程内**重复（迁移 246 的偏唯一索引
	//     uq_products_project_sku_code）。
	// 两条路都回带这一条（新建预检与编辑预检各一次），并且**唯一索引冲突的兜底也必须映射到它** ——
	// 直接把 23505 的原文（索引名 / SQLSTATE）铺到页面上既读不懂，也把库结构泄了出去（CQ-009）。
	ErrContainerSKUTaken = "ErrContainerSKUTaken"
)

// 商品列表「库存」列的三态（docs/14 §1.4，2026-09-19 口径）。
//
// 一个状态只用一种表达方式：「无限」不写进数量列，「还没入库」也不写成一行 0。
// 三态是**互斥**的，混合状态（有的仓无限、有的仓跟踪）按「无限」显示 ——
// 求和等于把无限当 0，那是最糟的一种错（页面显示有货、其实卖不完）。
const (
	// StockStateInfinite 任一仓 track_quantity = false（不跟踪 = 无限）→ 显示 ∞。
	StockStateInfinite = "infinite"
	// StockStateTracked 全部仓都跟踪 → 显示各仓数量之和（0 就显示 0）。
	StockStateTracked = "tracked"
	// StockStateNone 该商品在任何仓都没有库存行 → 显示「未入库」。
	StockStateNone = "none"
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

// 定价规则类型（issue #13）：只接受这四个内置类型 + 各自的白名单参数，
// 不接受自由表达式。算出的售价**落库**（product_variants.price），
// 构建期读的是落库后的确定值 —— 定价工具不参与构建管线。
const (
	// PricingRuleCostMultiple 成本乘倍数：售价 = 成本 × multiplier。
	PricingRuleCostMultiple = "cost_multiple"
	// PricingRuleCostMarkup 成本加价：售价 = 成本 + amount。
	PricingRuleCostMarkup = "cost_markup"
	// PricingRuleTargetMargin 目标毛利率：售价 = 成本 ÷ (1 - margin)（margin 为 0~1 的目标毛利率）。
	PricingRuleTargetMargin = "target_margin"
	// PricingRuleFixedPrice 统一售价：售价 = amount（不看成本）。
	PricingRuleFixedPrice = "fixed_price"
)

// 尾数处理（issue #13）：一律**向上**取（只抬不降），保证按规则算出的价格不会被尾数处理压低。
const (
	// PricingRoundingNone 不舍入：四舍五入到分。
	PricingRoundingNone = "none"
	// PricingRoundingInteger 向上取整到元。
	PricingRoundingInteger = "integer"
	// PricingRoundingEnd9 尾数 9：向上取到角位为 9（12.34 → 12.90）。
	PricingRoundingEnd9 = "end_9"
	// PricingRoundingEnd99 尾数 99：向上取到分为 99（12.34 → 12.99）。
	PricingRoundingEnd99 = "end_99"
)

// 定价工具的作用范围（issue #13）。
const (
	// PricingScopeSKU 单个 SKU（一个变体）。
	PricingScopeSKU = "sku"
	// PricingScopeProduct 单个商品的全部变体。
	PricingScopeProduct = "product"
	// PricingScopeFilter 筛选出的商品集（其全部变体）。
	PricingScopeFilter = "filter"
)

// 逐变体试算行的处理结果（预览与留痕共用）。
const (
	// PricingLineChanged 价格发生变化。
	PricingLineChanged = "changed"
	// PricingLineUnchanged 试算结果与当前价格相同（不写库、不留痕）。
	PricingLineUnchanged = "unchanged"
	// PricingLineSkipped 该变体本轮无法计算（原因见 Reason）。
	PricingLineSkipped = "skipped"
)

// 逐变体跳过原因（issue #13；带成本类规则在缺成本价时跳过单个变体，
// 其余变体照常处理 —— 一个没填成本的 SKU 不该让整批调价失败）。
const (
	// PricingSkipCostMissing 变体没有成本价（NULL），按成本类规则无法计算。
	PricingSkipCostMissing = "PricingSkipCostMissing"
	// PricingSkipOutOfRange 计算结果为负或超过可存储上限（numeric(12,2)）。
	PricingSkipOutOfRange = "PricingSkipOutOfRange"
)

// 响应消息与业务错误（定价工具，issue #13）。
const (
	// MsgPricingPreviewSuccess 试算完成（未落库）。
	MsgPricingPreviewSuccess = "MsgPricingPreviewSuccess"
	// MsgPricingApplySuccess 调价已应用（落库 + 留痕）。
	MsgPricingApplySuccess = "MsgPricingApplySuccess"

	// ErrPricingRuleTypeInvalid 定价规则类型不是内置类型。
	ErrPricingRuleTypeInvalid = "ErrPricingRuleTypeInvalid"
	// ErrPricingRuleParamsInvalid 定价规则参数不合法（未知键 / 类型不符 / 取值越界）。
	ErrPricingRuleParamsInvalid = "ErrPricingRuleParamsInvalid"
	// ErrPricingRoundingInvalid 尾数处理方式不是内置类型。
	ErrPricingRoundingInvalid = "ErrPricingRoundingInvalid"
	// ErrPricingScopeInvalid 作用范围不是内置类型。
	ErrPricingScopeInvalid = "ErrPricingScopeInvalid"
	// ErrPricingTargetRequired 该作用范围缺少目标（单个 SKU / 单个商品必须给目标 id）。
	ErrPricingTargetRequired = "ErrPricingTargetRequired"
	// ErrPricingTargetNotFound 定价目标（SKU 或商品）不存在。
	ErrPricingTargetNotFound = "ErrPricingTargetNotFound"
	// ErrPricingFilterEmpty 筛选集没有命中任何商品（拒绝「空筛选全表改价」）。
	ErrPricingFilterEmpty = "ErrPricingFilterEmpty"
	// ErrPricingNothingChanged 没有任何变体的价格需要改动（不写库、不留痕）。
	ErrPricingNothingChanged = "ErrPricingNothingChanged"
	// ErrPricingAdjustmentNotFound 调价批次不存在。
	ErrPricingAdjustmentNotFound = "ErrPricingAdjustmentNotFound"
	// ErrPricingTargetTooMany 筛选集命中的商品数超过单批上限（拒绝「一次改太多」）。
	ErrPricingTargetTooMany = "ErrPricingTargetTooMany"
)

// --- 批量操作的结论文案（页面回执，不是错误白名单）---
//
// 与 admin / order 的 Bulk* 同口径：值 = sys_i18n 的 item_key，**不带 Err / Msg 前缀**。
// 这些句子是 handler 按计数拼出的整句回执，不是 service 返回的错误，因此不进
// productenums.ProductErrFallbacks 那套「错误 key → 展示原文」的表。
//
// 传输已经改了：后台写动作走 ui/write-base.js 直调 /api/product/*，结论文案由基座取
// data-msg-* / data-msg-template 属性或 handler 回的 message 承担，**不再**渲染整页提示页。
// 因此下表多数常量已无 Go 消费者（见各分组的说明），但**词条与常量都留着**：
// 词条在 sys_i18n 里由迁移落地，删词条属于数据变更、要另起迁移，而保留的成本只是一条
// 无人读的行；常量留着则让「这句文案讲过什么」在代码里可检索。
//
// 还活着的只有两处：BulkPricingNoneSelected（空勾选守卫）与 BulkVariant*（变体清单保存
// 的合成句，中文原文在 inbound/http/product_bulk_handle.go 的 productVariantSave* 里）。
const (
	// 批量删除：五个实体各一对（部分成功 / 全部成功）。
	//
	// Tag / Attr / Brand / Product 四组的 Go 消费者已随页面写路由删除（批量删除改直调
	// /api/product/{delete,bulk-delete}，结论由 JSON 回执的 message 与 items 承担）；
	// Category 一组更早迁移（见下方注释）。
	BulkTagPartial  = "product.bulk.tagPartial"
	BulkTagDone     = "product.bulk.tagDone"
	BulkAttrPartial = "product.bulk.attrPartial"
	BulkAttrDone    = "product.bulk.attrDone"
	// BulkCategoryPartial / BulkCategoryDone 的 Go 消费者已随分类写动作迁移删除
	// （页面 POST → /api/product/category/bulk-delete，结论句由 write-base.js 的
	// data-msg-* 承担）。词条本身保留：它在 sys_i18n 里由迁移 283 落地，删词条属于
	// 数据变更、要另起迁移，而保留的成本只是一条无人读的行。
	BulkCategoryPartial = "product.bulk.categoryPartial"
	BulkCategoryDone    = "product.bulk.categoryDone"
	BulkBrandPartial    = "product.bulk.brandPartial"
	BulkBrandDone       = "product.bulk.brandDone"
	BulkProductPartial  = "product.bulk.productPartial"
	BulkProductDone     = "product.bulk.productDone"

	// 批量改价：没勾选 / 四种结论。
	//
	// NoneSelected 是活的（BulkPricing 的空勾选守卫）；其余四条结论句改用 JSON 回执的
	// message + data.skipped/items，Go 侧已无消费者。
	BulkPricingNoneSelected = "product.bulk.pricingNoneSelected"
	BulkPricingNoChange     = "product.bulk.pricingNoChange"
	BulkPricingApplied      = "product.bulk.pricingApplied"
	BulkPricingAllSkip      = "product.bulk.pricingAllSkip"
	BulkPricingPartial      = "product.bulk.pricingPartial"

	// 变体清单保存：无变化 / 已保存 / 跳过段。
	BulkVariantNoChange = "product.bulk.variantSaveNoChange"
	BulkVariantSaved    = "product.bulk.variantSaveSaved"
	BulkVariantSkipped  = "product.bulk.variantSaveSkipped"

	// 商品编辑页保存成功的回执已随页面 POST 消失（写动作走 ui/write-base.js 直调
	// /api/product/update，成功后基座 reload 编辑页）—— 常量刻意保留：词条在 449 迁移里，
	// 删它要连带删 seed，而这条回执正是基座将来要复用的一句。
	MsgProductSaved = "product.msg.saved"
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrInvalidParam` 这样的裸 key。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
// 对账由 internal/architecture 的 error_fallback_ownership_test.go 双向守。
var errFallbacks = map[string]string{
	ErrInvalidParam:              "请求参数无效",
	ErrBulkRejected:              "这一批里有不允许的操作，整批未生效：请检查标出的项目后重试",
	ErrInvalidType:               "不支持的内容类型",
	ErrInvalidField:              "内容字段不在白名单",
	ErrNotFound:                  "请求资源不存在",
	ErrSlugTaken:                 "同类型下 slug 已存在",
	ErrSkuTaken:                  "同商品下 SKU 编码已占用，请换一个",
	ErrVariantHasStock:           "该变体仍有库存，不能删除；请改为停用",
	ErrNameRequired:              "商品名称必填",
	ErrCommentPurchaseRequired:   "购买过该商品才能发表评论",
	ErrMissingProjectContext:     "构建期拿不到工程上下文，无法读取实体字段（属装配缺陷，请联系管理员）",
	ErrRelatedInvalid:            "相关商品引用不合法：引用的商品必须存在、属于本工程，且不能指向自己",
	ErrAttrNameRequired:          "属性组名称必填",
	ErrAttrKeyRequired:           "属性组标识不能改为空",
	ErrAttrKeyTaken:              "同工程下属性组标识已占用，请换一个",
	ErrAttrNotFound:              "属性组不存在",
	ErrAttrProjectMismatch:       "该属性组不属于这个商品所在的工程",
	ErrAttrInUse:                 "属性组已被商品引用，不能删除",
	ErrAttrValueLabelMissing:     "属性值名称必填",
	ErrCategoryNameRequired:      "分类名称必填",
	ErrCategoryNotFound:          "分类不存在",
	ErrCategorySlugTaken:         "同工程下分类 slug 已占用，请换一个",
	ErrCategoryParentMismatch:    "父分类不属于该分类所在的工程",
	ErrCategoryCycle:             "不能把分类挂到自身或自己的下级分类下",
	ErrCategoryHasChildren:       "该分类下仍有子分类，不能删除",
	ErrCategoryInUse:             "分类已被商品引用，不能删除",
	ErrCategoryProjectMismatch:   "该分类不属于这个商品所在的工程",
	ErrBrandNameRequired:         "品牌名称必填",
	ErrBrandNotFound:             "品牌不存在",
	ErrBrandSlugTaken:            "同工程下品牌 slug 已占用，请换一个",
	ErrBrandInUse:                "品牌已被商品引用，不能删除",
	ErrBrandProjectMismatch:      "该品牌不属于这个商品所在的工程",
	ErrTagNameRequired:           "标签名称必填",
	ErrTagNotFound:               "标签不存在",
	ErrTagSlugTaken:              "同工程下标签 slug 已占用，请换一个",
	ErrTagProjectMismatch:        "该标签不属于这个商品所在的工程",
	ErrTagCrossProject:           "标签仍被其它工程的商品引用，不能删除",
	ErrTagKindInvalid:            "标签类型只支持 manual（手工）或 rule（自动）",
	ErrTagRuleNotAllowed:         "手工标签不能携带自动规则",
	ErrTagRuleTypeInvalid:        "规则类型不是内置类型，不支持自定义表达式",
	ErrTagRuleParamsInvalid:      "规则参数不合法：键、类型或取值范围有问题",
	ErrTagNotManual:              "自动标签的归属由规则重算维护，不能手工挂载",
	ErrCollectionSourceInvalid:   "集合源标识不合法：不是本模块提供的集合源",
	ErrCollectionFilterInvalid:   "过滤维度不在该集合源允许的范围内",
	ErrVariationNoDimension:      "没有可用于生成变体的属性值，请先为商品填好属性",
	ErrVariationDimensionLimit:   "参与变体的属性维度超过上限，请减少参与生成变体的属性组",
	ErrVariationCountLimit:       "生成的变体组合数超过上限，请减少属性值或维度",
	ErrVariationAttributeInvalid: "勾选的属性组没有参与该商品的变体，不能用于生成",
	ErrVariationValueInvalid:     "勾选的属性值不属于该属性组，或已被停用",
	ErrVariationSelectionEmpty:   "未勾选任何属性值",
	ErrVariantSKUEmpty:           "变体的 SKU 编码不能为空：请填写一个编码（或改回系统生成的编码）",
	ErrVariantOptionsInvalid:     "变体的规格组合不合法：属性组或属性值不属于该商品",
	ErrSkuContainerMissing:       "SKU 编码无法生成：商品 URL 段不含 ASCII 字符，请显式填写 SKU 编码",
	ErrContainerSkuInvalid:       "主体 SKU 不合法：必须是非空字符串",
	ErrContainerSKUTaken:         "该主体 SKU 在本工程已被其它商品占用（主体编码在工程内唯一）",
	ErrPricingRuleTypeInvalid:    "定价规则类型不是内置类型",
	ErrPricingRuleParamsInvalid:  "定价规则参数不合法：未知键、类型不符或取值越界",
	ErrPricingRoundingInvalid:    "尾数处理方式不是内置类型",
	ErrPricingScopeInvalid:       "作用范围不是内置类型",
	ErrPricingTargetRequired:     "该作用范围需要指定目标：单个 SKU 或单个商品必须给出目标 id",
	ErrPricingTargetNotFound:     "定价目标（SKU 或商品）不存在",
	ErrPricingFilterEmpty:        "筛选没有命中任何商品，为避免全表改价已中止",
	ErrPricingNothingChanged:     "没有任何变体的价格需要改动，未写库也未留痕",
	ErrPricingAdjustmentNotFound: "调价批次不存在",
	ErrPricingTargetTooMany:      "筛选命中的商品数超过单批上限，请缩小范围后重试",
}
