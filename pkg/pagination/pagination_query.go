package pagination

// pagination_query.go — 列表页的**取址**与**页码收敛**（纯变换，零 gin、零 i18n）。
//
// 这三件事原先在商品模块的 inbound/http 里各写一份（listFilterQuery / productListBaseURL /
// clampPageToTotal / listPageSlice），别的模块要同样的行为只能再抄一遍 —— 抄出来的第二份
// 迟早与第一份分叉，而症状是「同一个筛选，翻页后关键词丢了」这种只在某一页发生的事。
// 落在这里之后，模块侧只保留自己的**筛选字段名**。

import (
	"net/url"
	"strings"
)

// DefaultPageSize 列表页在调用方没给 size（或给了 <1）时的每页条数。
//
// 与模板层 pager 的默认一致：两处不同值时会出现「表格每页 20、分页条按 10 算」。
const DefaultPageSize = 20

// BaseURL 把筛选条件拼到页面路径上（**不含** page / limit：分页条自己拼）。
//
// 为什么不能把 page 塞进基地址：分页条会在基地址后面再接 page，于是 URL 里出现两个
// page 参数，浏览器取第一个 —— 用户看到的是「点了翻页没反应」。
func BaseURL(path string, q url.Values) string {
	if q == nil {
		return path
	}
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// Query 成对构造筛选查询串：Query("project", id, "keyword", kw)。
//
// 空值（含全空白）一律跳过 —— 空参数会出现在分页链接里（?project=&keyword=），
// 既难看也会让「有筛选」的判断误命中。末尾落单的键直接忽略。
func Query(kv ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		if v := strings.TrimSpace(kv[i+1]); v != "" {
			q.Set(kv[i], v)
		}
	}
	return q
}

// ClampPage 把页码收敛到实际总页数以内（total=0 或 size<1 时按 DefaultPageSize 算）。
//
// 收敛必须发生在**取数之前**：page=999 时不先收敛，会出现「表格为空、分页条却显示
// 第 999 页」这种自相矛盾的组合。
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

// Slice 切出「第 page 页」并返回**收敛后**的页码（已经在内存里的切片分页）。
//
// 取数已经沉到 service 的页面不该再用它 —— 那是「先取全部再切」的读法；它留给
// 「一次查询本来就返回全集」的列表（如受限分类树）。
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
