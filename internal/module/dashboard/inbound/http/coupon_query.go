package dashboardhttp

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"

	"go_wp/internal/middleware/builtin"
)

// coupon_query.go - 优惠码管理页的表单取值、回跳链接与对外文案出口。

// couponSaveReqFromForm 从表单读取优惠码保存请求。
//
// 字段名必须与 CouponSaveReq 的 form tag 逐字一致：写错不会报错，只会绑定到零值
// （表现是「时间窗填了却被当成不限」「门槛被清零」这类静默的错误数据）。
func couponSaveReqFromForm(c *gin.Context) *orderdto.CouponSaveReq {
	return &orderdto.CouponSaveReq{
		ID:            orderQueryID(c.PostForm("id")),
		ProjectID:     strings.TrimSpace(c.PostForm("projectId")),
		Code:          strings.TrimSpace(c.PostForm("code")),
		Name:          strings.TrimSpace(c.PostForm("name")),
		DiscountType:  strings.TrimSpace(c.PostForm("discountType")),
		DiscountValue: couponFormInt64(c, "discountValue"),
		MinSubtotal:   couponFormInt64(c, "minSubtotal"),
		MaxUses:       couponFormInt(c, "maxUses"),
		PerUserLimit:  couponFormInt(c, "perUserLimit"),
		StartsAt:      strings.TrimSpace(c.PostForm("startsAt")),
		EndsAt:        strings.TrimSpace(c.PostForm("endsAt")),
		Status:        couponFormInt(c, "status"),
		Remark:        strings.TrimSpace(c.PostForm("remark")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorID:   couponOperatorID(c),
		OperatorName: builtin.GetUsername(c),
	}
}

// couponFormInt 读取整数表单字段；缺失 / 非法一律当 0（由服务端业务规则决定 0 是否可接受）。
//
// 负数**不在这里吞掉**：填了 -5 就应该让服务端报「优惠值不合法」，
// 静默改成 0 会让运营以为保存成功了，而券的配置并不是他填的那个。
func couponFormInt(c *gin.Context, key string) int {
	v, err := strconv.Atoi(strings.TrimSpace(c.PostForm(key)))
	if err != nil {
		return 0
	}
	return v
}

// couponFormInt64 读取 int64 表单字段（金额一律整数分）。
func couponFormInt64(c *gin.Context, key string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(c.PostForm(key)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// couponOperatorID 当前登录管理员 id（写进券的 create_by / update_by）。
func couponOperatorID(c *gin.Context) uint64 {
	if id := builtin.GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

// —— 表单与文案工具 ——

// couponBackKeys 允许在回跳 URL 里透传的查询键（与页面 GET 认的参数一致）。
var couponBackKeys = map[string]bool{
	"project": true, "status": true, "keyword": true,
	"page": true, "limit": true, "couponId": true,
}

// couponBackQuery 当前页面的回跳查询串（写操作表单的一个隐藏域）。
func couponBackQuery(projectID string, filter couponFilter, page, limit int, couponID uint64) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("status", filter.Status)
	set("keyword", filter.Keyword)
	set("page", strconv.Itoa(page))
	set("limit", strconv.Itoa(limit))
	if couponID > 0 {
		q.Set("couponId", strconv.FormatUint(couponID, 10))
	}
	return q.Encode()
}

// couponFacingError 把订单模块的错误转成可展示文案。
//
// 订单模块的业务错误本来就是给运营看的中文（「这个优惠码已经存在」），但它同时也可能是
// 数据库错误的原文（带表名甚至 SQL 片段）。因此先放行模块自己声明的
// orderenums.UserFacingMessages，再放行本页登记的管理侧文案，其余一律落到统一提示。
func couponFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := couponFacingText(err.Error()); msg != "" {
		return msg
	}
	return orderInternalText(c)
}

// couponFacingText 白名单校验：命中返回原文，未命中返回空串。
func couponFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if hit := orderFacingText(msg); hit != "" {
		return hit
	}
	for _, allowed := range couponFacingExtras {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

// couponQueryText 查询参数回显（?err= / ?ok=）：同样过白名单，
// 未命中时用 fallback（错误提示落统一文案，成功提示落空串）——
// 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
func couponQueryText(c *gin.Context, raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := couponFacingText(raw); msg != "" {
		return msg
	}
	return fallback
}
