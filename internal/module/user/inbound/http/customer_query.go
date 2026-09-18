package userhttp

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/sitetz"
	"net/url"
)

// customer_query.go - 客户管理页的查询参数解析、URL 派生与对外文案出口。
//
// 本文件里的解析函数与 user_customer_admin_handle.go（/api/customer/*）里同名的一组
// 刻意各自独立：接口面与页面面的取值口径不同（页面用 -1 表示「全部」，接口用
// userdto.CustomerStatusAll），合并会逼着两边共用一份「谁改都得动」的实现。
// 名字因此带上 Page 前缀以区别于接口面那一组。

// customerDetailBackURL 「返回列表」链接：只保留筛选条件（把详情专属参数留在详情页）。
// c 为 nil 时退化成纯列表路径：渲染数据组装是纯函数，理应在没有请求上下文时也能跑
// （渲染测试就是直接喂数据走这条路径的），不该因为少一个 context 就 panic。
func customerDetailBackURL(c *gin.Context) string {
	q := url.Values{}
	if c != nil {
		for _, key := range []string{"keyword", "status", "emailVerified", "locked", "registeredFrom", "registeredTo", "page", "limit"} {
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
	return shell.PageInternalText(c)
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

// customerFirstNonEmpty 取第一个非空文案（多处「提示只留第一条」的收口）。
func customerFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// customerQueryID 解析 id / customerId（非法即 0）。
func customerQueryID(raw string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// customerPageStatus 解析状态（空串或非法一律「全部」）。
//
// 空串**不能**落成 0：0 是「已停用」，那会让不带参数的请求只看到停用账号。
func customerPageStatus(raw string) int {
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

// customerPageLocked 解析「只看锁定」筛选（只有 ?locked=1 为真）。
//
// 与状态筛选是两条轴：被锁定的账号 status 仍是「正常」（锁定只写 locked_until_time），
// 所以它必须是一个独立的查询参数，不能拿状态下拉去表达。
func customerPageLocked(raw string) bool {
	return strings.TrimSpace(raw) == "1"
}

// customerPageEmailVerified 解析邮箱验证筛选（空串 → 全部）。
func customerPageEmailVerified(raw string) int {
	switch strings.TrimSpace(raw) {
	case "1":
		return userdto.EmailVerifiedYes
	case "2":
		return userdto.EmailVerifiedNo
	default:
		return userdto.EmailVerifiedAll
	}
}

// customerPageDayStart / customerPageDayEnd 日期字符串 → 当天的起止时刻（解析不了返回 nil = 该端不限）。
//
// 结束日期必须扩到当天最后一刻：把 2026-09-30 当成 00:00:00，那一天注册的客户
// 一个都筛不出来，而运营以为自己筛的是「到 9 月 30 日为止」—— 少一天看起来完全正常。
//
// 解析失败按「不限」处理而不是报错：这是筛选条件，拼错了退化成不筛，
// 比让整页变成错误页更接近运营的预期（他至少还看得到列表）。
func customerPageDayStart(raw string) *time.Time {
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

func customerPageDayEnd(raw string) *time.Time {
	day := customerPageDayStart(raw)
	if day == nil {
		return nil
	}
	end := day.AddDate(0, 0, 1).Add(-time.Nanosecond)
	return &end
}
