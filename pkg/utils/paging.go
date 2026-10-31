package utils

type Paging struct {
	Page   int
	Size   int
	Offset int
}

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

	if size > 0 && page-1 <= maxInt/size {
		offset = (page - 1) * size
	}
	return Paging{Page: page, Size: size, Offset: offset}
}

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
