// masterdata_page_util.go — 后台变更记录页共用的小工具（页面专属）。
package masterdatahttp

import "strings"

// firstNonEmpty 返回第一个非空白值（页面在「本次查询错误」与「?err= 回显」之间取先到者）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
