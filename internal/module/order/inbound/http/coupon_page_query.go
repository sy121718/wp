package orderhttp

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/web/shell"
)

// coupon_page_query.go - 优惠码管理页的表单取值、回跳链接与对外文案出口。

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
		StartsAt:      couponFormTimeValue(c.PostForm("startsAt")),
		EndsAt:        couponFormTimeValue(c.PostForm("endsAt")),
		Status:        couponFormInt(c, "status"),
		Remark:        strings.TrimSpace(c.PostForm("remark")),
		// 操作人由会话覆盖写入，绝不受表单影响。
		OperatorID:   shell.CurrentUserID(c),
		OperatorName: builtin.GetUsername(c),
	}
}

// couponFormTimeValue 归一化时间窗表单值，把控件提交的写法换成服务端认识的写法。
//
// <input type="datetime-local"> 提交的是它自己的 value 形态 ——「2026-01-01T09:00」（带 T），
// 而优惠码服务端的解析布局只有「2006-01-02」/「2006-01-02 15:04(:05)」（service 的
// couponTimeLayouts，禁改），带 T 的写法会被判成「生效时间不合法」。
// 不在这儿转换，就是**表单自己生成的格式自己都不收**：控件换对了、保存反而全废。
//
// 转换只在这一处发生（表单解析归口），做法是「解析成功才重新格式化」而不是字符串替换 ——
// 位置判断会把「2026-01-01 09:00」之外的畸形串也当成可转，解析失败则原样透传：
//
//	· 空串 → 原样（留空 = 不限，由 service 解释）
//	· 纯日期「2006-01-02」→ 原样（不支持 datetime-local 的浏览器退化写法 / 老客户端）
//	· 空格写法「2006-01-02 15:04」→ 原样（既有客户端仍在用）
func couponFormTimeValue(raw string) string {
	s := strings.TrimSpace(raw)
	// 带秒的输入保留秒（服务端同样接受「2006-01-02 15:04:05」）——
	// 统一截断到分等于静默改掉用户填的时刻。
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.Format("2006-01-02 15:04:05")
	}
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return t.Format("2006-01-02 15:04")
	}
	return s
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

// couponPageFacingText 页面路径的提示取词出口（白名单判定 + **取当前语言的译文**）。
//
// 与订单页 / 退货页同因（见 order_page_query.go 的 orderPageFacingText）：
// 白名单里存的是 item_key（order.msg.couponCreated / order.err.couponCodeTaken …），
// 而页面上的 {{.Ok}} / {{.Err}} 是直接渲染的文本、不经过 pkg/response 的 translate ——
// 只放行 key 的话，运营点「保存」后提示条上显示的就是 order.msg.couponUpdated 这串 key。
//
// 白名单判定仍只有一份（couponFacingText），本函数只把命中的值按当前语言取词：
// 命中的是 item_key 就出译文；命中的是本页自造的中文常量（couponIDInvalidText /
// couponForeignText）时按 key 查不到词条，取词函数据 fallback 原样返回。
func couponPageFacingText(c *gin.Context) func(string) string {
	return func(raw string) string {
		hit := couponFacingText(raw)
		if hit == "" {
			return ""
		}
		return shell.TranslateFor(c)(hit, hit)
	}
}

// couponFacingError 把订单模块的错误转成可展示文案。
//
// 订单模块的业务错误本来就是给运营看的中文（「这个优惠码已经存在」），但它同时也可能是
// 数据库错误的原文（带表名甚至 SQL 片段）。因此先放行模块自己声明的
// orderenums.UserFacingMessages，再放行本页登记的管理侧文案，其余一律落到统一提示。
//
// 命中那一支经 couponPageFacingText 取译文：白名单里存的是 item_key，
// 页面直接渲染，不取词就显示裸 key。
func couponFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := couponPageFacingText(c)(err.Error()); msg != "" {
		return msg
	}
	return shell.PageInternalText(c)
}

// couponFacingText 白名单校验：命中返回原文，未命中返回空串。
//
// 只做判定、不取词：API 出口需要 key（交给 pkg/response 翻译），页面出口用
// couponPageFacingText 取词。判定只有这一份。
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
