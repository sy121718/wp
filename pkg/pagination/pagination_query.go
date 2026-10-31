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
