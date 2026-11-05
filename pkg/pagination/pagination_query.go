package pagination

import (
	"net/url"
	"strings"
)

const DefaultPageSize = 20

func BaseURL(path string, q url.Values) string {
	if q == nil {
		return path
	}
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

func Query(kv ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		if v := strings.TrimSpace(kv[i+1]); v != "" {
			q.Set(kv[i], v)
		}
	}
	return q
}

func ClampPage(page, size int, total int64) int {
	if size < 1 {
		size = DefaultPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(size) - 1) / int64(size))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
}

func Slice[T any](all []T, page, size int) ([]T, int) {
	if size < 1 {
		size = DefaultPageSize
	}
	page = ClampPage(page, size, int64(len(all)))
	from := (page - 1) * size
	if from > len(all) {
		from = len(all)
	}
	to := from + size
	if to > len(all) {
		to = len(all)
	}
	return all[from:to], page
}

// NormalizePaging 夹取页码/页大小并给出偏移。
//
// 数值分页的唯一实现：各域默认值与上限由调用方传入，偏移溢出返回 MaxInt。
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

// Paging 是一次分页计算的归一结果。
type Paging struct {
	Page   int
	Size   int
	Offset int
}

// PageOffset 只算偏移（页码越界夹到第 1 页，溢出返回 MaxInt）。
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

// NormalizeLimitOffset 夹取 limit/offset。
func NormalizeLimitOffset(limit, offset, defaultLimit, maxLimit int) (int, int) {
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
