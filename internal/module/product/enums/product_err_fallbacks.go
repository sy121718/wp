package productenums

import "go_wp/pkg/facing"

// product_err_fallbacks.go — 商品域**业务错误**词条的 key 与「词条缺失时的中文原文」。
//
// 为什么住 enums 而不是控制器（product_jump.go）：key 的归属在本目录，「兜底原文与 key 是一对」；
// 分两处住时改一句文案要动控制器，而控制器只该交**事实**。同形先例见本目录的
// product_translation_fallbacks.go 与 product_err_detail.go（page 模块的 pageenums.ErrDetailFallbacks
// 是同一形态的样板）。
//
// 三件事的关系（task-158 §7.3 ⑤）：
//
//	① key —— sys_i18n 的 (item_key, lang) 主键。取词按请求语言查它，命中的那句就是页面显示的；
//	② 本表 —— **只在 i18n 未初始化 / sys_i18n 里没有这个 key 的词条**时用的展示原文；
//	③ HTTP 状态码与响应格式（400 / 404 / 500、JSON 体形状）**不在本包**，留在
//	   internal/module/product/inbound/http（response.ErrorAuto / c.JSON 那一层）—— 换个出口
//	   （HTMX 片段 / JSON / 提示页）时状态码各按各的规矩，而文案只有这一份。
//
// 理想形态是本表与 seed 的 zh-CN 词条**逐字一致**（不一致时线上取的是 seed 那句，本表只在缺词条时
// 生效）；所以改一句文案要 seed 与本表一起改，漏改的表现是「测试环境显示一句、线上显示另一句」。
//
// 性质说明：这张表**不是可透出性判据** —— 判据只有一份，在 pkg/facing.IsBusinessKey
// （pkg/response.IsBusinessError 转发它）。本表只回答「显示什么」，不回答「能不能显示」。
//
// 覆盖面守卫：product_err_fallbacks_test.go 用 AST 扫本包常量 —— 每条 Err* 都必须在本表里有原文
// （漏一条即失败），而 VariantSkip* / BundleMember* 这类「跳过原因」不许登记（它们的取值过不了
// 业务判据，兜底表在出口层）—— 新加业务错误不会静默退化成归口文案或裸 key。

// ProductErrFallbacks 商品域业务错误的展示兜底：key 是 enums 常量（取值即 sys_i18n 的 item_key）。
//
// 本表只在 service 声明处被读：`productenums.Err(key)` 用它给错误配兜底（见文件末尾），
// 出口（页面 / JSON / HTMX 片段）只认 pkg/facing.FactOf 交出来的事实，不查表。
//
// 表里没有的 key 由出口处理：
//
//	· inventory / presentation 的 key —— 兜底在 product 的 HTTP 层（见 product_jump.go 的
//	  productErrForeignFallbacks，那边写明了为什么暂时留在这儿）；
//	· 没登记的业务 key —— 出口给稳定的一句归口文案并记一条日志，绝不把裸 key 摆到页面上。
var ProductErrFallbacks = map[string]string{
	ErrInvalidParam:                  "请求参数无效",
	ErrNotFound:                      "商品或变体不存在",
	ErrSlugTaken:                     "该 URL 段在本工程已被占用",
	ErrSkuTaken:                      "该 SKU 编码在本商品下已被占用",
	ErrNameRequired:                  "商品名称必填",
	ErrAttrNotFound:                  "属性组不存在",
	ErrAttrProjectMismatch:           "属性组不属于该商品所在工程",
	ErrProductTypeInvalid:            "商品类型不合法（仅支持变体商品或捆绑商品）",
	ErrProductTypeImmutable:          "商品类型在创建后不可更改",
	ErrBundlePriceRequired:           "捆绑商品必须自定价：容器价需大于 0",
	ErrSkuContainerMissing:           "SKU 编码无法生成：商品 URL 段不含 ASCII 字符，请显式填写 SKU 编码",
	ErrContainerSkuInvalid:           "主体 SKU 不合法：必须是非空字符串",
	ErrBundleSKURequired:             "捆绑商品必须填写主体 SKU 编码（新建抽屉已给出建议值，可直接修改）",
	ErrContainerSKUTaken:             "该主体 SKU 在本工程已被其它商品占用（主体编码在工程内唯一）",
	ErrVariantSKUEmpty:               "变体的 SKU 编码不能为空：请填写一个编码（或改回系统生成的编码）",
	ErrVariantOptionsInvalid:         "变体的规格组合不合法：属性组或属性值不属于该商品",
	ErrVariantHasStock:               "变体仍有库存，不能删除（请改为停用）",
	ErrBundleSourceInvalid:           "成员来源不合法：只支持「从商品导入」「从仓库选」「自选属性值组合」",
	ErrBundleSourceProductRequired:   "该来源必须先选一个来源商品",
	ErrBundleSourceWarehouseRequired: "从仓库选时必须指定仓库并至少勾选一条仓库 SKU",
	ErrBundleSelfReference:           "捆绑成员不能引用捆绑主体自己（自引用）",
	ErrPricingRuleTypeInvalid:        "定价规则类型不是内置类型",
	ErrPricingRuleParamsInvalid:      "定价规则参数不合法（参数键 / 类型 / 取值范围）",
	ErrPricingRoundingInvalid:        "尾数处理不是内置选项",
	ErrPricingScopeInvalid:           "定价作用范围不合法",
	ErrPricingTargetRequired:         "该作用范围必须指定目标（变体 id 或商品 id）",
	ErrPricingTargetNotFound:         "商品或变体不存在、没有可改价的变体（捆绑容器只有容器价，不能按规则改价）",
	ErrPricingFilterEmpty:            "筛选集至少要给一个筛选条件",
	ErrPricingNothingChanged:         "按该规则算下来没有任何价格变化",
	ErrPricingTargetTooMany:          "命中的商品数超过单批上限，请收紧筛选条件",
	ErrPricingAdjustmentNotFound:     "调价批次不存在",
	ErrAttrInUse:                     "属性组已被商品引用，不能删除",
	ErrAttrKeyRequired:               "属性组标识不能改为空",
	ErrAttrKeyTaken:                  "该属性组标识在本工程已被占用",
	ErrAttrNameRequired:              "属性组名称必填",
	ErrBrandInUse:                    "品牌已被商品引用，不能删除",
	ErrBrandNameRequired:             "品牌名称必填",
	ErrBrandNotFound:                 "品牌不存在",
	ErrBrandProjectMismatch:          "品牌不属于该商品所在工程",
	ErrBrandSlugTaken:                "该品牌 URL 段在本工程已被占用",
	ErrBundleMaxOptionsInvalid:       "捆绑选项数量上限不合法（允许范围 1~20）",
	ErrBundleNotConfigured:           "该商品没有配置捆绑选项（不是捆绑商品）",
	ErrBundleOptionRequired:          "漏填了捆绑的必选项",
	ErrBundleOptionsExceeded:         "捆绑选项数超过上限",
	ErrBundleQtyAboveMax:             "某一项的数量超过该项最大数量",
	ErrBundleQtyAboveStock:           "某一项的数量超过该仓可用库存",
	ErrBundleQtyBelowMin:             "某一项的数量低于该项最小数量",
	ErrBundleQtyInvalid:              "数量不合法（负数 / 非整数 / 超过硬上限）",
	ErrBundleQtyRangeInvalid:         "某一项的数量区间自相矛盾（最小 > 默认、最大 < 最小等）",
	ErrBundleShapeInvalid:            "捆绑配置形状非法（既不是空值，也不是 JSON 对象）",
	ErrBundleStockUnavailable:        "读不到库存可用量（库存能力未装配），捆绑校验拒绝通过",
	ErrBundleTotalAboveMax:           "整单总件数超过最大总件数",
	ErrBundleTotalBelowMin:           "整单总件数低于最小总件数",
	ErrBundleTotalRangeInvalid:       "整单件数区间自相矛盾（最大 < 最小，或低于必选项最小量之和）",
	ErrBundleTotalUnreachable:        "整单下限高于所有选项能加到的上限（永远无法满足）",
	ErrBundleVariantDuplicated:       "同一个 SKU 在配置里或同一次选择里出现多次",
	ErrBundleVariantNotFound:         "选项引用的 SKU 不存在（可能已被删除）",
	ErrBundleVariantNotInConfig:      "选择里出现了配置之外的 SKU",
	ErrBundleVariantProjectMismatch:  "选项引用的 SKU 不属于该商品所在工程",
	ErrBundleVariantRequired:         "捆绑选项缺少 SKU",
	ErrCategoryCycle:                 "不能把分类挂到自身或自己的后代下",
	ErrCategoryHasChildren:           "分类仍有子级，不能删除",
	ErrCategoryInUse:                 "分类已被商品引用，不能删除",
	ErrCategoryNameRequired:          "分类名称必填",
	ErrCategoryNotFound:              "分类不存在",
	ErrCategoryParentMismatch:        "父分类不属于该分类所在工程",
	ErrCategoryProjectMismatch:       "分类不属于该商品所在工程",
	ErrCategorySlugTaken:             "该分类 URL 段在本工程已被占用",
	ErrCollectionFilterInvalid:       "过滤维度不在集合源白名单内",
	ErrCollectionSourceInvalid:       "集合源标识不合法（不是本模块实现的源）",
	ErrInvalidField:                  "提交了不支持的字段（不在商品字段白名单内）",
	ErrInvalidType:                   "实体类型不合法",
	ErrRelatedInvalid:                "相关商品引用不合法：引用的商品必须存在、属于本工程，且不能指向自己",
	ErrTagCrossProject:               "标签仍被其它工程的商品引用，不能删除",
	ErrTagKindInvalid:                "标签类型不是 manual / rule",
	ErrTagNameRequired:               "标签名称必填",
	ErrTagNotFound:                   "标签不存在",
	ErrTagNotManual:                  "自动标签的归属由重算维护，不能手工挂载",
	ErrTagProjectMismatch:            "标签不属于该商品所在工程",
	ErrTagRuleNotAllowed:             "手工标签不能带自动规则",
	ErrTagRuleParamsInvalid:          "规则参数不合法（键 / 类型 / 取值范围）",
	ErrTagRuleTypeInvalid:            "规则类型不是内置类型（不接受自由表达式）",
	ErrTagSlugTaken:                  "该标签 URL 段在本工程已被占用",
	ErrVariationAttributeInvalid:     "勾选的属性组未参与该商品的变体",
	ErrVariationCountLimit:           "变体组合数超过上限",
	ErrVariationDimensionLimit:       "参与变体的属性维度超过上限",
	ErrVariationNoDimension:          "没有可参与变体的属性值",
	ErrVariationValueInvalid:         "勾选的属性值不属于该属性组或已停用",
	ErrVariationSelectionEmpty:       "未勾选任何属性值：请至少勾选一个属性值，或改用「生成全部组合」",

	// —— 完整性补齐（task-158 §7.3 用 AST 对账扫出来的三条）——
	// 它们此前没有兜底：一旦词条缺失，页面上会出现裸 key（ErrAttrValueLabelMissing 这种串，
	// 运营只会以为后台坏了）。文案与 seed 的 zh-CN 词条逐字一致（改文案要两处一起改）。
	ErrAttrValueLabelMissing:   "属性值名称必填",                            // 迁移 430_i18n_enums_missing_keys.sql
	ErrMissingProjectContext:   "构建期拿不到工程上下文，无法读取实体字段（属装配缺陷，请联系管理员）", // 迁移 430
	ErrCommentPurchaseRequired: "购买过该商品才能发表评论",                       // 迁移 468_product_comment_policy_i18n.sql
}

// Err 把本包的 key 变成**自带中文兜底**的错误（兜底取 ProductErrFallbacks）。
//
// 兜底绑在声明处（本目录）之后，出口只调一次共享判据 pkg/facing.FactOf：
// 页面出口 adminui.PageErrorFact / PageErrorText 与 JSON 出口 response.ErrorAuto
// 读同一份事实，控制器不再各查一张表。加一条业务错误 = 本包加常量 + 表里加一句文案。
//
// 复合错误用 fmt.Errorf("%w: %s", Err(key), 明细)：facing.FactOf 按 ASCII 冒号切出明细段
// （Tail），并保留 errors.As 能拿到的 key 与兜底。
func Err(key string) error {
	return facing.New(key, ProductErrFallbacks[key])
}
