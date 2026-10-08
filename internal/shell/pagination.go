package shell

// pagination.go — 分页条数据的**转发**（实现在 pkg/pagination）。
//
// 为什么实现不在本包：这里是「把 total/page/limit 变成页码按钮列表」的纯数据变换，
// 没有 gin、没有模板、没有 i18n 状态 —— 放在 shell 会让模板侧用不了它
//（`internal/shell` 反向依赖 `internal/templates`，templates 不能再 import shell）。
//
// 保留这些别名与转发是为了不动 46 个调用点与既有测试的写法（shell.BuildPagination /
// shell.PageLink）；新代码可以直接用 pkg/pagination。
import "go_wp/pkg/pagination"

type (
	// PageLink 分页条上的单个链接（页码 / 上一页 / 下一页 / 省略号）。
	PageLink = pagination.PageLink
	// PaginationData 分页数据；total 为 0 或单页时为 nil。
	PaginationData = pagination.PaginationData
)

// maxPageLinks 页码按钮最大数量（超出用省略号）。
const maxPageLinks = pagination.MaxPageLinks

// BuildPagination 构造分页数据；total 为 0 或单页时返回 nil。详见 pkg/pagination。
func BuildPagination(total int64, page, limit int, baseURL string,
	t func(key, fallback string) string) *pagination.PaginationData {
	return pagination.BuildPagination(total, page, limit, baseURL, t)
}
