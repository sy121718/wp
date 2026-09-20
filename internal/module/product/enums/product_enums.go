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

	ErrInvalidParam    = "ErrInvalidParam"    // 参数错误
	ErrInvalidType     = "ErrInvalidType"     // 实体类型不合法（构建期字段解析）
	ErrInvalidField    = "ErrInvalidField"    // 字段不在商品字段白名单内
	ErrNotFound        = "ErrNotFound"        // 商品或变体不存在
	ErrSlugTaken       = "ErrSlugTaken"       // 同工程下 slug 已占用
	ErrSkuTaken        = "ErrSkuTaken"        // 同商品下 SKU 编码已占用
	ErrVariantHasStock = "ErrVariantHasStock" // 变体仍有库存，不能删除（请改为停用）
	ErrNameRequired    = "ErrNameRequired"    // 商品名称必填
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

	// 保存时被跳过的一行（不整批失败，逐条回带原因；值即 i18n key）。
	// 与定价工具的 PricingSkip* 同一形态：跳过是「这一行没做」，不是「整批失败」。
	VariantSkipHasStock       = "VariantSkipHasStock"       // 该变体仍有非零库存
	VariantSkipReferenced     = "VariantSkipReferenced"     // 该变体被 BOM 清单引用
	VariantSkipDuplicated     = "VariantSkipDuplicated"     // 清单里重复的规格组合（只保留第一行）
	VariantSkipVariantMissing = "VariantSkipVariantMissing" // 清单引用的既有变体已不存在（并发删除）

	// 删除守卫的另外两个引用面（docs/14 §8「待补」两项，迁移 259 配套）：
	//   · 捆绑成员引用 —— products.bundle_items.options[].variantId 指向这个变体；
	//   · 有过任何库存流水 —— 订单一旦建单就必然产生扣减流水，所以「有流水」等价于
	//     「被订单用过」（历史单据按 variant_id 追溯，不允许硬删）。
	// 与非零库存互补：卖出后补货清零的变体库存为 0 却仍被用过，两个守卫都要。
	VariantSkipBundleReferenced = "VariantSkipBundleReferenced" // 该变体被捆绑成员引用
	VariantSkipHasMovement      = "VariantSkipHasMovement"      // 该变体有过库存流水（被订单用过）

	// —— 商品主体 SKU 编码（2026-09-19 评审第四轮，迁移 248 配套）——
	// 两条都是「可行动的」错误：运营要么显式填一个编码，要么把 URL 段改成 ASCII。
	ErrSkuContainerMissing = "ErrSkuContainerMissing" // 商品 URL 段不含 ASCII 字符，派生不出主体 SKU 编码
	ErrContainerSkuInvalid = "ErrContainerSkuInvalid" // 显式填的主体 SKU 编码不合法（空串）
	// ErrBundleSKURequired 捆绑商品的主体 SKU 必填（2026-09-19 用户拍板，迁移 249 配套）。
	//
	// 与变体商品不同：捆绑不生成自己的变体，主体 SKU 就是它唯一的对外身份 ——
	// 旧实现「留空即静默按 <商品段>_B 派生」被否掉，理由是编码要**被运营看见并确认**；
	// 前台改为预填建议值、允许修改，服务端不再接受空值（静默派生会让运营以为没填也行）。
	ErrBundleSKURequired = "ErrBundleSKURequired" // 捆绑商品必须填写主体 SKU 编码

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

// 捆绑品（issue #20）。
//
// 配置期错误（保存时拒，运营填错了）与选择期错误（前台即时反馈，买家选错了）分开：
// 后台与前台因此都能给出具体原因，而不是一句笼统的「不合法」。
const (
	// MsgBundleSaveSuccess 捆绑配置已保存。
	MsgBundleSaveSuccess = "MsgBundleSaveSuccess"
	// MsgBundleValidateSuccess 整单校验通过。
	MsgBundleValidateSuccess = "MsgBundleValidateSuccess"

	// ErrProductTypeInvalid 商品类型不合法（仅支持 variant / bundle）。
	ErrProductTypeInvalid = "ErrProductTypeInvalid"
	// ErrProductTypeImmutable 商品类型不可在创建后更改（变体 ↔ 捆绑涉及存量数据差异）。
	ErrProductTypeImmutable = "ErrProductTypeImmutable"
	// ErrBundlePriceRequired 捆绑容器必须自定价（容器价 > 0）—— 成员价不参与计价，
	// 容器价为空就是「0 元套餐」，列表与详情会显示 0.00，前台也分不清是免费还是没配。
	ErrBundlePriceRequired = "ErrBundlePriceRequired"

	// ErrBundleShapeInvalid 捆绑配置形状非法（既不是空值，也不是 JSON 对象）。
	ErrBundleShapeInvalid = "ErrBundleShapeInvalid"
	// ErrBundleNotConfigured 该商品没有配置捆绑选项（不是捆绑品）。
	ErrBundleNotConfigured = "ErrBundleNotConfigured"
	// ErrBundleMaxOptionsInvalid 选项数量上限非法（超出 1..20 护栏）。
	ErrBundleMaxOptionsInvalid = "ErrBundleMaxOptionsInvalid"
	// ErrBundleOptionsExceeded 配置的选项数超过上限。
	ErrBundleOptionsExceeded = "ErrBundleOptionsExceeded"

	// ErrBundleVariantRequired 选项缺少 SKU（配置期与选择期共用）。
	ErrBundleVariantRequired = "ErrBundleVariantRequired"
	// ErrBundleVariantDuplicated 同一个 SKU 在配置里或同一次选择里出现多次。
	ErrBundleVariantDuplicated = "ErrBundleVariantDuplicated"
	// ErrBundleVariantNotFound 选项引用的 SKU 不存在（已被删除）。
	ErrBundleVariantNotFound = "ErrBundleVariantNotFound"
	// ErrBundleVariantProjectMismatch 选项引用的 SKU 不属于该商品所在工程。
	ErrBundleVariantProjectMismatch = "ErrBundleVariantProjectMismatch"
	// ErrBundleSelfReference 选项引用了捆绑主体自己的 SKU（自引用）。
	ErrBundleSelfReference = "ErrBundleSelfReference"
	// ErrBundleVariantNotInConfig 选择里出现配置之外的 SKU。
	ErrBundleVariantNotInConfig = "ErrBundleVariantNotInConfig"

	// ErrBundleQtyInvalid 数量非法（负数 / 非整数 / 超过硬上限）。
	ErrBundleQtyInvalid = "ErrBundleQtyInvalid"
	// ErrBundleQtyRangeInvalid 单项数量区间自相矛盾（最小 > 默认、最大 < 最小 等）。
	ErrBundleQtyRangeInvalid = "ErrBundleQtyRangeInvalid"
	// ErrBundleTotalRangeInvalid 整单件数区间自相矛盾（最大 < 最小，或低于必选项最小量之和）。
	ErrBundleTotalRangeInvalid = "ErrBundleTotalRangeInvalid"
	// ErrBundleTotalUnreachable 整单下限高于所有选项能加到的上限（永远无法满足）。
	ErrBundleTotalUnreachable = "ErrBundleTotalUnreachable"

	// ErrBundleOptionRequired 漏填必选项。
	ErrBundleOptionRequired = "ErrBundleOptionRequired"
	// ErrBundleQtyBelowMin 单项数量低于该项最小数量。
	ErrBundleQtyBelowMin = "ErrBundleQtyBelowMin"
	// ErrBundleQtyAboveMax 单项数量超过该项最大数量。
	ErrBundleQtyAboveMax = "ErrBundleQtyAboveMax"
	// ErrBundleTotalBelowMin 整单总件数低于最小总件数。
	ErrBundleTotalBelowMin = "ErrBundleTotalBelowMin"
	// ErrBundleTotalAboveMax 整单总件数超过最大总件数。
	ErrBundleTotalAboveMax = "ErrBundleTotalAboveMax"

	// ErrBundleQtyAboveStock 数量超过库存可用量（只读库房真源得出的结论）。
	ErrBundleQtyAboveStock = "ErrBundleQtyAboveStock"
	// ErrBundleStockUnavailable 读不到库存可用量（inventory 端口未注入）：整单校验 fail-closed。
	ErrBundleStockUnavailable = "ErrBundleStockUnavailable"
)

// 捆绑成员的三种来源（docs/14 §1.2，批次 C）。
//
// 成员引用的**永远是变体**（inventory_stocks.variant_id 是 NOT NULL，
// 所以「从仓库选」一定能定位到变体；刻意不造「无变体的成员」）。
// 来源只是**溯源与展示**，不是身份：身份恒为 variantId。
//
// 三种来源都走同一条出口（ResolveBundleMembers）：去重、单条失败不整批失败、
// 组合由服务端重算（不信任前端提交的行）。
const (
	// BundleSourceProduct 从商品导入：选中一个商品 → 其全部启用变体一次导入为成员。
	BundleSourceProduct = "BundleSourceProduct"
	// BundleSourceWarehouse 从仓库选：按仓列出仓库 SKU，选中即定位到该仓那条货的变体。
	BundleSourceWarehouse = "BundleSourceWarehouse"
	// BundleSourceAttributes 自选属性值笛卡尔积：服务端按属性组固定顺序重算组合，
	// 只接受「商品侧确实存在对应变体」的组合。
	BundleSourceAttributes = "BundleSourceAttributes"
)

// 捆绑成员来源解析的业务错误与逐条跳过原因（常量值即 i18n key）。
//
// 跳过原因是「这一条没加进来」，不是「整批失败」—— 与变体清单的 VariantSkip* 同一口径：
// 一条失败不能把整批回滚（运营会以为「一条都没加」然后反复重试）。
const (
	// ErrBundleSourceInvalid 来源标识不合法（不是 product / warehouse / attributes）。
	ErrBundleSourceInvalid = "ErrBundleSourceInvalid"
	// ErrBundleSourceProductRequired 该来源必须先选一个来源商品。
	ErrBundleSourceProductRequired = "ErrBundleSourceProductRequired"
	// ErrBundleSourceWarehouseRequired 从仓库选时必须指定仓库并至少给一条仓库 SKU。
	ErrBundleSourceWarehouseRequired = "ErrBundleSourceWarehouseRequired"

	// BundleMemberNotOnProduct 该属性值组合在商品侧**没有对应变体** ——
	// 明确拒绝并在结果里逐条列出（提示先到商品上生成该规格的变体），绝不静默丢弃、
	// 也不造无变体成员（成员的身份恒为 variantId）。
	BundleMemberNotOnProduct = "BundleMemberNotOnProduct"
	// BundleMemberSkippedInList 该变体已经在成员清单里（同一变体只出现一次）。
	BundleMemberSkippedInList = "BundleMemberSkippedInList"
	// BundleMemberWarehouseSKUMissing 该仓没有这条仓库 SKU（库存行不存在）。
	BundleMemberWarehouseSKUMissing = "BundleMemberWarehouseSKUMissing"
	// BundleMemberVariantDisabled 该变体已停用（停用的 SKU 挂进套餐会变成
	// 前台「选不了又躲不开」的必选项，与 ListBundleSKUs 只列启用变体同一口径）。
	BundleMemberVariantDisabled = "BundleMemberVariantDisabled"
	// BundleMemberOptionsExceeded 加到配置的选项数量上限了（其余候选逐条列出，不静默截断）。
	BundleMemberOptionsExceeded = "BundleMemberOptionsExceeded"
)

// --- 批量操作的结论文案（页面回执，不是错误白名单）---
//
// 与 admin / order 的 Bulk* 同口径：值 = sys_i18n 的 item_key，**不带 Err / Msg 前缀**。
// 这些句子是 handler 按计数拼出的整句回执（进 ?done= / ?err=），不是 service 返回的错误，
// 因此不进 productErrFallbacks / productErrSentinels 那套错误白名单 ——
// 它们走的是 product_err.go 的 productNoticeTexts 回执白名单。
//
// 中文原文全部留在 inbound/http/product_err.go（与写侧共用的那份 bulkTemplate 表绑在一起），
// 这里的常量只声明 key。
const (
	// 批量删除：五个实体各一对（部分成功 / 全部成功）。
	BulkTagPartial      = "product.bulk.tagPartial"
	BulkTagDone         = "product.bulk.tagDone"
	BulkAttrPartial     = "product.bulk.attrPartial"
	BulkAttrDone        = "product.bulk.attrDone"
	BulkCategoryPartial = "product.bulk.categoryPartial"
	BulkCategoryDone    = "product.bulk.categoryDone"
	BulkBrandPartial    = "product.bulk.brandPartial"
	BulkBrandDone       = "product.bulk.brandDone"
	BulkProductPartial  = "product.bulk.productPartial"
	BulkProductDone     = "product.bulk.productDone"

	// 批量改价：没勾选 / 四种结论。
	BulkPricingNoneSelected = "product.bulk.pricingNoneSelected"
	BulkPricingNoChange     = "product.bulk.pricingNoChange"
	BulkPricingApplied      = "product.bulk.pricingApplied"
	BulkPricingAllSkip      = "product.bulk.pricingAllSkip"
	BulkPricingPartial      = "product.bulk.pricingPartial"

	// 变体清单保存：无变化 / 已保存 / 跳过段。
	BulkVariantNoChange = "product.bulk.variantSaveNoChange"
	BulkVariantSaved    = "product.bulk.variantSaveSaved"
	BulkVariantSkipped  = "product.bulk.variantSaveSkipped"
)
