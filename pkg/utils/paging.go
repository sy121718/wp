package utils

import "go_wp/pkg/pagination"

// Paging 保留既有类型名，定义为分页包的归一结果。
type Paging = pagination.Paging

// NormalizePaging 数值分页统一到 pkg/pagination。
func NormalizePaging(page, size, defaultSize, maxSize int) Paging {
	return pagination.NormalizePaging(page, size, defaultSize, maxSize)
}

// PageOffset 数值分页偏移统一到 pkg/pagination。
func PageOffset(page, size int) int { return pagination.PageOffset(page, size) }

// NormalizeLimitOffset 夹取 limit/offset（统一实现）。
func NormalizeLimitOffset(limit, offset, defaultLimit, maxLimit int) (int, int) {
	return pagination.NormalizeLimitOffset(limit, offset, defaultLimit, maxLimit)
}
