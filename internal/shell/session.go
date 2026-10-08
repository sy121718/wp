package shell

// session.go — 后台页面「当前操作人」的统一取值入口。
//
// 为什么要有单点：操作人 id 此前在每个模块各写一份（adminPageOperatorID / operatorID /
// couponOperatorID / orderOperatorID / pricingOperatorID 五种实现，返回值类型都不一样：
// uint64 与 string 混用），另有十余处直接手写 c.Get("user_id") 的类型断言。
// 语义相同却各写一遍，改一处判断口径要满仓找。
//
// 取值口径统一走 builtin.GetUserID（它带类型断言保护，异常降级为 0），
// 这里只做「int64 → uint64」与「负数不成立」的收口。

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
)

// CurrentUserID 取当前登录管理员 id；未登录或取值异常返回 0。
//
// 返回 0 表示「取不到操作人」——调用方据此决定是否跳过需要操作人身份的判定
// （例如 admin 的「不能删自己」）。**需要审计归属时不要忽略 0**：它意味着
// 这条操作没有可追溯的操作人。
func CurrentUserID(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

// CurrentUserIDText 取当前登录管理员 id 的十进制文本；未登录返回空串。
//
// 给 OperatorID 声明为 string 的调用方用（如商品定价的审计字段），
// 避免它们为了一次转换各写一份 strconv。
func CurrentUserIDText(c *gin.Context) string {
	id := CurrentUserID(c)
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}
