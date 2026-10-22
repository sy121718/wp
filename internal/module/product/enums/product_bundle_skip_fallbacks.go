package productenums

// product_bundle_skip_fallbacks.go — 成员来源解析「这一行被跳过」的中文原文。
//
// 为什么这张表住 enums 而不是 service：它和 ProductErrFallbacks 是同一件事的另一种形态 ——
// key 与「词条缺失时显示什么」是一对，声明处只能是 enums。住 service 的代价是「改一句文案
// 要动业务层」，而业务层只该交事实。
//
// 与 ProductErrFallbacks 分成两张表，判据不是风格：
//   - 这张表的取值（BundleMemberNotOnProduct 这类）过不了 pkg/facing.IsBusinessKey，
//     它们不是错误消息，而是「某一行为什么没被加进清单」的逐行原因；
//   - 因此它们也不会经 productErrFactOf 被查到，混进 ProductErrFallbacks 就是死条目
//     （product_err_fallbacks_test.go 的 forbidden 分支会因此变红）。
//
// 取值 → 原文的对应由 product_bundle_skip_fallbacks_test.go 与 enums 常量双向对账。
var BundleMemberSkipFallbacks = map[string]string{
	BundleMemberNotOnProduct:        "该属性值组合在商品侧没有对应变体，未加入（请先到该商品上生成这个规格的变体）",
	BundleMemberSkippedInList:       "该 SKU 已在成员清单里，未重复加入",
	BundleMemberWarehouseSKUMissing: "该仓库里没有这条仓库 SKU，未加入（请确认仓库选对了，或先在该仓建好这条货）",
	BundleMemberVariantDisabled:     "该变体已停用，未加入（停用的 SKU 挂进套餐会变成前台选不了又躲不开的必选项）",
	BundleMemberOptionsExceeded:     "已达该捆绑配置的选项数量上限，未加入（可先调大上限再解析）",
	ErrBundleSelfReference:          "该 SKU 属于这个捆绑容器自己，未加入（不能自引用）",

	// —— 变体清单保存时被跳过的行（VariantSkip*）——
	//
	// 与 BundleMember* 同形：这一行为什么没被写进去，不是整批失败的原因。
	VariantSkipHasStock:       "仍有库存，未删除（请先处理库存或改为停用）",
	VariantSkipReferenced:     "被 BOM 清单引用，未删除（请先解除引用）",
	VariantSkipDuplicated:     "与清单里前面的行重复，已忽略",
	VariantSkipVariantMissing: "这一行对应的变体已不存在（可能已被别处删除）",
	// 本批新增的两个引用面（迁移 259）：被捆绑成员引用 / 有过库存流水。
	VariantSkipBundleReferenced: "被捆绑成员引用，未删除（请先在捆绑配置里解除引用）",
	VariantSkipHasMovement:      "有过库存流水（已被订单或库存变动用过），未删除（请改为停用）",
}

// BundleMemberSkipFallback 取一条跳过原因的中文原文（未登记的 key 返回空串）。
//
// 空串的含义是「这张表漏了这条原因」，由测试守着；调用方（响应出口）据此仍能取到词条
// —— pkg/response 在兜底为空时退回 key 本身，不会输出空文案。
func BundleMemberSkipFallback(key string) string { return BundleMemberSkipFallbacks[key] }
