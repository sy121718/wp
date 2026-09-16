package dashboardhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	orderenums "go_wp/internal/module/order/enums"

	"go_wp/internal/middleware/builtin"
)

// order_query.go - 订单管理页的查询参数解析与对外文案出口。

// orderListWindow 解析列表窗口：以 page/limit 为准（与分页组件一致），
// 兼容只有 offset 的链接（offset 换算成页码，每页条数取同一个 limit）。
func orderListWindow(c *gin.Context) (page, limit int) {
	page, limit = pageParams(c)
	if strings.TrimSpace(c.Query("page")) != "" {
		return page, limit
	}
	if offset, err := strconv.Atoi(strings.TrimSpace(c.Query("offset"))); err == nil && offset > 0 {
		page = offset/limit + 1
	}
	return page, limit
}

// orderOperatorID 当前登录管理员 id（写进状态流转记录的操作人 id）。
func orderOperatorID(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

// orderQueryID 解析 orderId 查询参数（非法即 0 = 不渲染详情块）。
func orderQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// orderFacingError 把订单模块的错误转成可展示文案。
//
// 订单模块的业务错误本来就是给运营看的中文（「库存不足，无法下单」），但它同时也
// 可能是数据库错误的原文（带表名甚至 SQL 片段）。因此只放行模块自己声明的
// orderenums.UserFacingMessages 白名单，其余一律落到统一提示。
func orderFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := orderFacingText(err.Error()); msg != "" {
		return msg
	}
	return pageInternalText(c)
}

// orderFacingText 白名单校验：命中返回原文，未命中返回空串。
func orderFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	for _, allowed := range orderenums.UserFacingMessages {
		if msg == allowed {
			return msg
		}
	}
	return ""
}
