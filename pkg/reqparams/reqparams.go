// Package reqparams 请求参数与表单值的**形状收敛**：把 HTTP 请求带回来的字符串收敛成
// 代码能直接用的值，判据只有这一份。
//
// 为什么要有独立包：这些函数原先与后台页面外壳（internal/shell）混在一起 —— 于是
// 「解析 ?page= / ?limit=」「取一个表单字段」这种与后台毫无关系的事，被绑上了整个后台
// 外壳的依赖面。抽出来之后：**不依赖 templates、不依赖任何业务模块契约、不 import
// internal**（pkg 不得反向依赖），只依赖 gin 与标准库。
//
// 判据（改这里等于改所有页面的入参口径，先读再改）：
//
//   - **缺省与上限是口径，不是巧合**：PageParams 缺省第 1 页 / 每页 20 / 上限 100
//     （与各模块 GetLimit() 的上限一致）。
//   - **非法输入一律降级为零值，不报错**：ParseUint / ParseStatus 取 0；
//     ParseStatusPtr 在**非空但非法**时取「非 nil 的 0」（`Atoi` 失败后 v 仍是 0 再取址）
//     —— 这是调用方依赖的老口径，迁移时逐字保留，不许顺手「修正」成 nil。
//   - **值一律 TrimSpace 后解析**：表单里混进空格/换行是常态。
//   - **FilterBaseURL 只丢空值**：非空值原样进 query（编码交给 url.Values），
//     它不判断值的合法性 —— 那是各页面的白名单与 pkg/safepath 的事。
package reqparams

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// IsHXRequest reports whether HX-Request is true, ignoring case and surrounding whitespace.
func IsHXRequest(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

// FieldValue 取 PostForm 值并去掉首尾空白；无值返回空串。
func FieldValue(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

// ParseUint 解析非负整数；空串或非法返回 0。
//
// 一处容易看漏的老口径：**溢出返回 MaxUint64，不是 0** —— `strconv.ParseUint` 在 ErrRange
// 时把值夹到上限并返回，而这里忽略错误直接返回该值（`-1` / `abc` 这类非数字才是 0）。
// 迁移逐字保留：把它「修」成 0 会改变超长 id 的落点（从「查不到」变成「id=0」），
// 那是行为变更，不是整理。
func ParseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

// ParseStatus 解析状态；空串返回 0（多数域 0=禁用，service 默认启用由各 create 处理）。
func ParseStatus(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

// ParseStatusPtr 解析状态为 *int（未传用 nil 表示，如 RoleCreate 的「未传即启用」）。
//
// 注意「未传」（空串 → nil）与「传了但非法」（→ 指向 0 的指针）是两回事：后者沿用
// 原 shell 实现的降级口径（`Atoi` 失败后仍取址），不是 bug 也不是笔误。
func ParseStatusPtr(s string) *int {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	v, _ := strconv.Atoi(t)
	return &v
}

// FilterBaseURL 拼出「路径 + 非空筛选参数」作为分页链接前缀，翻页时保留筛选条件。
func FilterBaseURL(path string, filters map[string]string) string {
	q := url.Values{}
	for k, v := range filters {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// PageParams 读取分页查询参数（?page=&limit=），缺省第 1 页、每页 20 条。
// limit 上限 100（与各模块 GetLimit() 的上限一致）。
func PageParams(c *gin.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.Query("page"))
	limit, _ = strconv.Atoi(c.Query("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
