// product_page_util.go — 商品后台页的表单取值小工具。
package producthttp

import "strconv"

// parseFloat 解析表单里的金额字段（非法值返回错误，由调用方忽略）。
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
