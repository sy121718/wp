package productenums

// product_skip_fallbacks.go — 变体清单保存时「这一行被跳过」的中文原文。
//
// 为什么这张表住 enums 而不是 service：它和 ProductErrFallbacks 是同一件事的另一种形态 ——
// key 与「词条缺失时显示什么」是一对，声明处只能是 enums。住 service 的代价是「改一句文案
// 要动业务层」，而业务层只该交事实。
//
// 与 ProductErrFallbacks 分成两张表，判据不是风格：这张表的取值（VariantSkipHasStock 这类）
// 过不了 pkg/facing.IsBusinessKey，它们不是错误消息，而是「某一行为什么没被写进去」的逐行
// 原因；因此它们也不会经 productErrFactOf 被查到，混进 ProductErrFallbacks 就是死条目
// （product_err_fallbacks_test.go 的 forbidden 分支会因此变红）。
//
// 取值 → 原文的对应由 product_skip_fallbacks_test.go 与 enums 常量双向对账。
var VariantSkipFallbacks = map[string]string{
	VariantSkipHasStock:       "仍有库存，未删除（请先处理库存或改为停用）",
	VariantSkipDuplicated:     "与清单里前面的行重复，已忽略",
	VariantSkipVariantMissing: "这一行对应的变体已不存在（可能已被别处删除）",
	// 迁移 259 新增的引用面：有过库存流水（卖出后补货清零的变体库存为 0 却仍被用过）。
	VariantSkipHasMovement: "有过库存流水（已被订单或库存变动用过），未删除（请改为停用）",
}

// VariantSkipFallback 取一条跳过原因的中文原文（未登记的 key 返回空串）。
//
// 空串的含义是「这张表漏了这条原因」，由测试守着；调用方（响应出口）据此仍能取到词条
// —— pkg/response 在兜底为空时退回 key 本身，不会输出空文案。
func VariantSkipFallback(key string) string { return VariantSkipFallbacks[key] }
