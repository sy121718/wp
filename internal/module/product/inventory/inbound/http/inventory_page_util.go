// inventory_page_util.go — 后台库存页共用的表单小工具（页面专属，不进 API 路径）。
package inventoryhttp

import (
	"strconv"
	"strings"
)

// parseIntOr 解析十进制整数，失败返回兜底值（后台表单容错，不因一个脏字段 500）。
func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}
