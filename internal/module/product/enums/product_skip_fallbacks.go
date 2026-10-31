package productenums

var VariantSkipFallbacks = map[string]string{
	VariantSkipHasStock:       "仍有库存，未删除（请先处理库存或改为停用）",
	VariantSkipDuplicated:     "与清单里前面的行重复，已忽略",
	VariantSkipVariantMissing: "这一行对应的变体已不存在（可能已被别处删除）",

	VariantSkipHasMovement: "有过库存流水（已被订单或库存变动用过），未删除（请改为停用）",
}

func VariantSkipFallback(key string) string { return VariantSkipFallbacks[key] }

const (
	VariantSkipHasStock       = "VariantSkipHasStock"
	VariantSkipDuplicated     = "VariantSkipDuplicated"
	VariantSkipVariantMissing = "VariantSkipVariantMissing"
	VariantSkipHasMovement    = "VariantSkipHasMovement"
)
