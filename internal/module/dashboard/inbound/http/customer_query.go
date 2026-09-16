package dashboardhttp

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/sitetz"
	"net/url"
)

// customer_query.go - 客户管理页的查询参数解析、URL 派生与对外文案出口。

// customerDetailBackURL 「返回列表」链接：只保留筛选条件（把详情专属参数留在详情页）。
// c 为 nil 时退化成纯列表路径：渲染数据组装是纯函数，理应在没有请求上下文时也能跑
// （渲染测试就是直接喂数据走这条路径的），不该因为少一个 context 就 panic。
func customerDetailBackURL(c *gin.Context) string {
	q := url.Values{}
	if c != nil {
		for _, key := range []string{"keyword", "status", "emailVerified", "registeredFrom", "registeredTo", "page", "limit"} {
			if v := strings.TrimSpace(c.Query(key)); v != "" {
				q.Set(key, v)
			}
		}
	}
	if len(q) == 0 {
		return customerListPath
	}
	return customerListPath + "?" + q.Encode()
}

// customerOrderURL 最近一单在订单管理页的展开链接（订单页靠 orderId 参数展开详情）。
func customerOrderURL(projectID string, orderID uint64) string {
	if orderID == 0 {
		return ""
	}
	q := url.Values{}
	if strings.TrimSpace(projectID) != "" {
		q.Set("project", projectID)
	}
	q.Set("orderId", strconv.FormatUint(orderID, 10))
	return "/admin/orders?" + q.Encode()
}

// customerDetailURL 某个客户的详情链接（保留不来 —— 返回时用「返回列表」回到筛选结果）。
func customerDetailURL(id uint64) string {
	return customerDetailPath + "?id=" + strconv.FormatUint(id, 10)
}

// customerFacingError 把 user 模块的错误转成可展示文案。
//
// 只放行 userenums.UserFacingMessages 白名单，其余一律落到统一提示：
// 未命中的通常是数据库错误的 Error()，带表名甚至 SQL 片段，那是给运维看的。
func customerFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := customerFacingText(err.Error()); msg != "" {
		return msg
	}
	return customerInternalText(c)
}

// customerFacingText 白名单校验：命中返回原文，未命中返回空串。
func customerFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	for _, allowed := range userenums.UserFacingMessages {
		if msg == allowed {
			return msg
		}
	}
	if _, ok := customerLocalMessageSet[msg]; ok {
		return msg
	}
	return ""
}

// customerQueryText 查询参数回显（?err= / ?ok=）：同样过白名单，
// 未命中时用 fallback（错误提示落统一文案，成功提示落空串）——
// 免得任何人手拼一个 URL 就能往页面上塞任意「提示」。
func customerQueryText(c *gin.Context, raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if msg := customerFacingText(raw); msg != "" {
		return msg
	}
	return fallback
}

// customerInternalText 统一内部错误文案（走当前语言的译文，缺词条回退中文原文）。
func customerInternalText(c *gin.Context) string {
	return translateFor(c)(dashboardenums.MsgInternalError, "系统内部错误，请稍后重试")
}

// customerQueryID 解析 id / customerId（非法即 0）。
func customerQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// customerQueryStatus 解析状态（空串或非法一律「全部」）。
//
// 空串**不能**落成 0：0 是「已停用」，那会让不带参数的请求只看到停用账号。
func customerQueryStatus(raw string) int {
	switch strings.TrimSpace(raw) {
	case "0":
		return customerStatusDisabled
	case "1":
		return customerStatusActive
	case "2":
		return customerStatusPending
	default:
		return customerStatusAll
	}
}

// customerQueryEmailVerified 解析邮箱验证筛选（空串 → 全部）。
func customerQueryEmailVerified(raw string) int {
	switch strings.TrimSpace(raw) {
	case "1":
		return userdto.EmailVerifiedYes
	case "2":
		return userdto.EmailVerifiedNo
	default:
		return userdto.EmailVerifiedAll
	}
}

// customerDayStart / customerDayEnd 日期字符串 → 当天的起止时刻（解析不了返回 nil = 该端不限）。
//
// 结束日期必须扩到当天最后一刻：把 2026-09-30 当成 00:00:00，那一天注册的客户
// 一个都筛不出来，而运营以为自己筛的是「到 9 月 30 日为止」—— 少一天看起来完全正常。
//
// 解析失败按「不限」处理而不是报错：这是筛选条件，拼错了退化成不筛，
// 比让整页变成错误页更接近运营的预期（他至少还看得到列表）。
func customerDayStart(raw string) *time.Time {
	v := strings.TrimSpace(raw)
	if v == "" {
		return nil
	}
	// 与 API 侧同一口径（pkg/sitetz）：日期筛选按站点时区解释，不跟随服务器时区。
	day, err := time.ParseInLocation("2006-01-02", v, sitetz.Location())
	if err != nil {
		return nil
	}
	return &day
}

func customerDayEnd(raw string) *time.Time {
	day := customerDayStart(raw)
	if day == nil {
		return nil
	}
	end := day.AddDate(0, 0, 1).Add(-time.Nanosecond)
	return &end
}
