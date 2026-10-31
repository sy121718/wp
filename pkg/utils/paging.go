package utils

// Paging 归一化后的分页参数（页码从 1 起算）。
type Paging struct {
	Page   int
	Size   int
	Offset int
}

// NormalizePaging 归一化页码与页大小，并计算 offset。
//
// page / size 非正时分别回落为 1 与 defaultSize；size 超过 maxSize 时封顶。
func NormalizePaging(page, size, defaultSize, maxSize int) Paging {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = defaultSize
	}
	if size > maxSize {
		size = maxSize
	}
	maxInt := int(^uint(0) >> 1)
	offset := maxInt
	// 超出可表示范围时保留越界语义，避免回绕到首页或负偏移。
	if size > 0 && page-1 <= maxInt/size {
		offset = (page - 1) * size
	}
	return Paging{Page: page, Size: size, Offset: offset}
}

// PageOffset 由页码与页大小算 offset；页码非正按 1 算，乘出可表示范围时饱和到 maxInt
// （与 NormalizePaging 同一口径：不回绕首页、不为负）。
//
// 为什么单列出来：页面 handler 与工具层各自手写 (page-1)*size，溢出时 Go 的乘法会回绕成
// 负数或小数，把「翻到很后面」变成「回到第一页」或「负偏移」。归一化只做一次，
// 口径只有这一处。
func PageOffset(page, size int) int {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/size {
		return maxInt
	}
	return (page - 1) * size
}

// NormalizeLimitOffset 归一化 limit/offset 分页（offset 从 0 起算）。
func NormalizeLimitOffset(limit, offset, defaultLimit, maxLimit int) (normLimit, normOffset int) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
