package dashboardhttp

import (
	"fmt"
	"strings"
	"time"
)

// order_labels.go - 订单管理页的展示文案映射（状态、操作人、来源、支付、金额、地址、时间）。

// orderStatusLabel 状态 → 中文标签（未知值原样返回：宁可显示生值，也不显示空白）。
func orderStatusLabel(status string) string {
	for _, view := range orderStatusViews {
		if view.Value == status {
			return view.Label
		}
	}
	if strings.TrimSpace(status) == "" {
		return orderFieldEmpty
	}
	return status
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
func orderOperatorTypeLabel(operatorType string) string {
	switch strings.TrimSpace(operatorType) {
	case "admin":
		return "后台"
	case "system":
		return "系统"
	case "customer", "user":
		return "客户"
	case "":
		return orderFieldEmpty
	default:
		return operatorType
	}
}

// orderCreatedViaLabel 下单入口 → 展示文案。
func orderCreatedViaLabel(createdVia string) string {
	switch strings.TrimSpace(createdVia) {
	case "checkout":
		return "访客结算"
	case "admin":
		return "后台自建"
	case "api":
		return "接口"
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
func orderAddressLabel(parts ...string) string {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			segments = append(segments, v)
		}
	}
	return strings.Join(segments, " ")
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
