// product_page_util.go — 商品后台页的表单取值小工具。
package producthttp

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// parseFloat 解析表单里的金额字段（非法值返回错误，由调用方忽略）。
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// productDetailLocation 商品详情页的写操作回跳地址（PRG）。
//
// 详情页里的写表单（变体 / 评分 / 属性引用 / 分类与品牌 / 手工标签）提交后必须留在详情页：
// 用户改的是**这个商品**的子资源，弹回列表页等于让他重新找一遍那个商品再点进来。
// errMsg 非空时作为 ?err= 回显（详情页模板已有错误提示位）——**成功与失败都回详情页**，
// 只有「商品本身被删掉」是例外（那时详情页没有意义，见 ProductsDelete）。
func productDetailLocation(projectID, productID, errMsg string) string {
	loc := "/admin/products/detail?project=" + url.QueryEscape(projectID) +
		"&product=" + url.QueryEscape(productID)
	if errMsg != "" {
		loc += "&err=" + url.QueryEscape(errMsg)
	}
	return loc
}

// formProductID 取表单里的商品 id：优先 productId，再回落 id。
//
// 两个名字都是**既有字段名**，不下令重命名：详情页的商品级表单（属性引用 / 分类与品牌 /
// 手工标签）用的隐藏域是 id（其值就是商品 id），子资源表单（变体 / 评分）用的是 productId。
// 只适用于这两者都指向商品本身的地方 —— 变体删除 / 评分删除的 id 是**子资源 id**，
// 那两个 handler 必须读 productId，绝不能走这里的回落。
func formProductID(c *gin.Context) string {
	if id := strings.TrimSpace(c.PostForm("productId")); id != "" {
		return id
	}
	return strings.TrimSpace(c.PostForm("id"))
}
