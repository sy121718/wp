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
	return Paging{
		Page:   page,
		Size:   size,
		Offset: (page - 1) * size,
	}
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
