package orderhttp

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderenums "go_wp/internal/module/order/enums"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

// order_page_labels.go - 订单管理页的展示文案映射（状态、操作人、来源、支付、金额、地址、时间）。

// translate 展示层的取词函数（与 shell.TranslateFor(c) 同形）。
//
// 类型别名（不是定义新类型）是为了能把 shell.TranslateFor(c) 直接传进来：
// 展示标签一律经它取词，**中文原文留在代码里当兜底** —— 词条缺失时页面显示中文，
// 而不是裸 key（`admin.orders.operator_type.admin`）或者英文界面恒中文。
type translate = func(key, fallback string) string

// orderLabel 一条展示标签（i18n key + 中文兜底）。
//
// 与 orderBulkText 同形：**key 与中文原文只有这一份**，取词只有一个入口（orderLabelOf）。
// 直接在函数体里写中文的后果是「模板直接渲染的文本永远中文」——
// orders.html 的 {{o.StatusLabel}} / {{detail.Head.StatusLabel}} 都是**直接渲染**，
// 不经过 pkg/response 的 translate，所以在 Go 侧硬写中文等于英文界面恒中文。
type orderLabel struct{ key, fallback string }

// orderLabelOf 取一条标签的当前语言文本；tr 为 nil（纯函数测试路径）时回落中文兜底。
func orderLabelOf(tr translate, l orderLabel) string {
	if tr == nil {
		return l.fallback
	}
	return tr(l.key, l.fallback)
}

// orderPlaceholderRE 已删除：命名占位符（`{name}`）的检测与填充复用 pkg/i18n
//（FillNamedPlaceholders / FillTranslate），本模块不再自造一份替换器。

// orderLabelFilled 取词并按命名参数填充（复用 pkg/i18n 的命名占位符实现）。
//
// 词条被改坏（填完仍有残留 `{name}`）时 FillTranslate 自动回落中文兜底再填一次 ——
// 页面上宁可显示代码里那句写死的中文，也不把 `{count}` 这样的字面量端给运营看。
// tr 为 nil（纯函数测试路径）时按默认语言之外的中文兜底取。
func orderLabelFilled(tr translate, l orderLabel, kv map[string]string) string {
	if tr == nil {
		tr = func(_, fallback string) string { return fallback }
	}
	return i18n.FillTranslate(tr, l.key, l.fallback, kv)
}

// 订单页的展示标签词条。
var (
	// orderPageTitleLabel 页面标题（sys_i18n 已有 admin.orders.heading，与其它列表页同口径）。
	orderPageTitleLabel = orderLabel{"admin.orders.heading", "订单管理"}
	// 状态计数条与筛选回显里的「全部」。
	//
	// 复用访客订单片段的 site.fragment.order.tab_all：同一个语义（订单状态的「全部」）
	// 在两个面上共用一条词条，好过再造一条同值的。
	orderStatusAllLabel = orderLabel{"site.fragment.order.tab_all", "全部"}
	// 操作人类型（状态流转链的「操作人类型」列）。
	orderOperatorTypeAdminLabel    = orderLabel{"admin.orders.operator_type.admin", "后台"}
	orderOperatorTypeSystemLabel   = orderLabel{"admin.orders.operator_type.system", "系统"}
	orderOperatorTypeCustomerLabel = orderLabel{"admin.orders.operator_type.customer", "客户"}
	// 下单入口（详情页的「入口」）。
	orderCreatedViaCheckoutLabel = orderLabel{"admin.orders.created_via.checkout", "访客结算"}
	orderCreatedViaAdminLabel    = orderLabel{"admin.orders.created_via.admin", "后台自建"}
	orderCreatedViaAPILabel      = orderLabel{"admin.orders.created_via.api", "接口"}
	// 订单编号不合法（表单里的 orderId 缺失 / 被改坏时回带的参数级提示）。
	orderInvalidIDLabel = orderLabel{"admin.orders.form.invalid_id", "订单编号不合法，请回到列表页重新操作。"}
)

// orderStatusLabel 状态 → 展示标签（未知值原样返回：宁可显示生值，也不显示空白）。
//
// 映射的真源在 orderenums.OrderStatusLabel（后台客户页经 ordercontract 引用**同一份**）：
// 三处各写一张中文表的结果是「改一处、另两处静默留在旧说法上」。
// key 为空表示这一档没有词条可查（空状态 / 认不出的取值），直接用兜底值 / 原值。
func orderStatusLabel(tr translate, status string) string {
	key, fallback := orderenums.OrderStatusLabel(status)
	if key == "" {
		if strings.TrimSpace(status) == "" {
			return orderFieldEmpty
		}
		return fallback
	}
	return orderLabelOf(tr, orderLabel{key, fallback})
}

// orderStatusBadge 状态 → 徽章样式。
func orderStatusBadge(status string) string {
	for _, view := range orderStatusViews {
		if view.Value == status {
			return view.Badge
		}
	}
	return "badge-mute"
}

// orderOperatorTypeLabel 操作人类型 → 展示文案。
func orderOperatorTypeLabel(tr translate, operatorType string) string {
	switch strings.TrimSpace(operatorType) {
	case "admin":
		return orderLabelOf(tr, orderOperatorTypeAdminLabel)
	case "system":
		return orderLabelOf(tr, orderOperatorTypeSystemLabel)
	case "customer", "user":
		return orderLabelOf(tr, orderOperatorTypeCustomerLabel)
	case "":
		return orderFieldEmpty
	default:
		return operatorType
	}
}

// orderCreatedViaLabel 下单入口 → 展示文案。
func orderCreatedViaLabel(tr translate, createdVia string) string {
	switch strings.TrimSpace(createdVia) {
	case "checkout":
		return orderLabelOf(tr, orderCreatedViaCheckoutLabel)
	case "admin":
		return orderLabelOf(tr, orderCreatedViaAdminLabel)
	case "api":
		return orderLabelOf(tr, orderCreatedViaAPILabel)
	case "":
		return orderFieldEmpty
	default:
		return createdVia
	}
}

// orderPaymentLabel 支付方式展示：通道标题优先，括号里补上通道代码（对账要看代码）。
func orderPaymentLabel(method, title string) string {
	label := strings.TrimSpace(title)
	code := strings.TrimSpace(method)
	switch {
	case label != "" && code != "":
		return label + "（" + code + "）"
	case label != "":
		return label
	case code != "":
		return code
	default:
		return orderFieldEmpty
	}
}

// orderMoneyLabel 金额（分）→ 带币种的展示文本。人民币用 ¥，其它币种用代码前缀。
func orderMoneyLabel(cents int64, currency string) string {
	amount := orderAmountText(cents)
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "", "CNY", "RMB":
		return "¥" + amount
	default:
		return strings.ToUpper(strings.TrimSpace(currency)) + " " + amount
	}
}

// orderAmountText 分 → 元文本（保留两位小数）。订单域金额一律整数分，
// 展示层的换算只有这一处；模板里不做任何算术。
func orderAmountText(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// orderAddressLabel 地址拼接：逐段丢掉空值（地址字段常常只填一半），
// 全空时返回空串由模板决定怎么显示。
//
// 调用方把**国家/地区排在第一位**（地址书写从大到小）。库里存的是下单时刻的
// 代码快照（迁移 501），传进来之前已经由 countryLabelFn 换成当前语言的显示名；
// 这一层**只拼接**，不认识字典也不做语言判断 —— 把它改成要 ctx 的方法，等于让
// 每个调用点都得先想清楚自己在什么语言下（那是解析器的事，不是拼接的事）。
func orderAddressLabel(parts ...string) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			segments = append(segments, v)
		}
	}
	return strings.Join(segments, " ")
}

// countryLabelFn 生成「国家/地区代码 → 当前界面语言显示名」的解析闭包。
//
// 返回 nil 表示**没有解析能力**（字典未注入 / 装配退化）：调用方经 applyCountryLabel
// 原样显示 code —— 「未接入」与「接入但查不到」刻意走同一条回落路径，这样页面在
// 两种情形下的表现一致（都是显示代码），不会出现「装配漏了」只在某个页面变成空白。
//
// 这里刻意不返回 error：展示标签的读取失败不该让订单详情整页 500 或变成一行报错。
// 字典读不到时记日志在 sysconfig 侧（那是它知道失败原因的地方），页面照常渲染。
//
// lang 取当前请求语言（response.RequestLanguage，与 sysconfig 后台页同源）。
func countryLabelFn(c *gin.Context, dict sysconfigcontract.DictReader) func(string) string {
	if c == nil || dict == nil {
		return nil
	}
	ctx := c.Request.Context()
	lang := response.RequestLanguage(c)
	return func(code string) string {
		code = strings.TrimSpace(code)
		if code == "" {
			return ""
		}
		if label := strings.TrimSpace(dict.CountryLabel(ctx, lang, code)); label != "" {
			return label
		}
		return code
	}
}

// applyCountryLabel 用解析闭包把代码换成显示名；闭包为 nil（未接入字典）时原样返回代码。
func applyCountryLabel(label func(string) string, code string) string {
	code = strings.TrimSpace(code)
	if label == nil || code == "" {
		return code
	}
	return label(code)
}

// orderTimeLabel 时间 → 后台展示文本（本地时区，分钟精度）。
func orderTimeLabel(at time.Time) string {
	if at.IsZero() {
		return orderFieldEmpty
	}
	return at.Local().Format("2006-01-02 15:04")
}

// orderTimeLabelPtr 可空时间 → 展示文本。
func orderTimeLabelPtr(at *time.Time) string {
	if at == nil {
		return orderFieldEmpty
	}
	return orderTimeLabel(*at)
}

// orderTextOrEmpty 空值统一显示成「—」。
func orderTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return orderFieldEmpty
	}
	return value
}
