package projectdto

// shipping_policy_dto.go — 站点级运费规则（projects.settings 里的两个键）。
//
// 两个决定写在这里，因为它们决定了整个功能的可用性：
//
//  1. **库内单位是分**（与 orders 表同口径）。元只出现在后台表单边界，
//     换算只有 ParseYuanToCents / FormatCentsAsYuan 两个函数，且全程整数拆分 ——
//     `int64(math.Round(ParseFloat(s)*100))` 在 0.29 这类值上会因二进制浮点表示
//     偶尔差 1 分（0.29*100 = 28.999999999999996），而「运费偶尔多一分」没有任何
//     可解释的成因，排查只能靠猜。
//
//  2. **判据单源**：保存（后台表单）与读取（结算路径的运费端口）都经
//     NormalizeShippingPolicy。两边各写一份校验必然分叉，而分叉的表现是
//     「后台存进去了、结算时却按不收运费处理」这种不报错、不记日志的静默失效
//     （形态照 builder.NormalizeGA4MeasurementID 的既有样板：保存与注入同一判据）。

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxShippingFeeCents 运费金额上限（分，= 1 万元）。
//
// 上限的作用是拦住误输入（多打几个零），不是防溢出：金额是 int64，
// 1 万与 1 亿在存储上没有区别，但一笔 1 亿元的运费一定是填错了 ——
// 而它会一路进订单总额、进支付扣款，属于「错得越离谱越难发现」的那一类。
const MaxShippingFeeCents int64 = 1_000_000

// 运费金额的解析失败原因（三类分开，让后台能把「哪里错」说清楚）。
//
// 合成一句「金额不合法」的代价：用户填了 -1 却只被告知「请填数字」——
// 他会继续试，因为他填的确实是数字。
var (
	// ErrShippingFeeFormat 不是合法数字（含小数位超过两位、千分位逗号等）。
	ErrShippingFeeFormat = errors.New("运费金额必须是不小于 0 的数字，最多两位小数")
	// ErrShippingFeeNegative 负数。
	ErrShippingFeeNegative = errors.New("运费金额不能为负数")
	// ErrShippingFeeTooLarge 超过 MaxShippingFeeCents。
	ErrShippingFeeTooLarge = errors.New("运费金额超出上限")
)

// ShippingPolicy 站点级运费规则（归一化后的结论，单位分）。
//
// 零值即「不收运费」：BaseFeeCents = 0 时不收费，门槛字段无意义
// （所以设置页里「配了门槛但没配运费」不是错误，只是冗余 —— 先配门槛后配运费
// 是很自然的填写顺序，把它判成非法只会让人换个顺序再填一遍）。
type ShippingPolicy struct {
	// BaseFeeCents 基础运费（分，≥ 0；0 = 不收运费）。
	BaseFeeCents int64
	// FreeThresholdCents 满额免运费门槛（分；0 = 不启用「满额免运费」）。
	//
	// > 0 时：订单商品小计达到该额即免基础运费。
	// 它与 BaseFeeCents 的高低**不做比较**：门槛是「买多少钱免运费」的独立商业参数，
	// 「满 50 元免 8 元运费」是最常见的配置，要求门槛必须高于基础运费会把它判成非法。
	FreeThresholdCents int64
}

// NormalizeShippingPolicy 归一化并校验运费规则（**保存与读取的唯一判据**）。
//
// 负数与超上限一律报错，**不静默归零**：负运费等于倒贴钱（订单总额会被减掉一笔），
// 而「把非法值悄悄改成 0」会让运营以为自己配的运费生效了 —— 实际从没生效过，
// 且页面上看不出任何异常。这两种后果都比「保存时报一句错」严重得多。
func NormalizeShippingPolicy(baseCents, thresholdCents int64) (p ShippingPolicy, err error) {
	if baseCents < 0 || thresholdCents < 0 {
		return ShippingPolicy{}, ErrShippingFeeNegative
	}
	if baseCents > MaxShippingFeeCents || thresholdCents > MaxShippingFeeCents {
		return ShippingPolicy{}, ErrShippingFeeTooLarge
	}
	return ShippingPolicy{BaseFeeCents: baseCents, FreeThresholdCents: thresholdCents}, nil
}

// ParseYuanToCents 把表单里的「元」转成「分」；空串 = 0（不配置）。
//
// 整数拆分：把 "12.34" 拆成整数位 12 与小数位 34，再算 12*100+34 ——
// 全程不碰浮点，所以 0.29 一定得到 29 分。
//
// 只接受「数字」或「数字.数字」两种形态：多余的小数点、字母、千分位逗号一律拒绝。
// 不去猜用户的意思（"1,000" 到底是一千还是一？猜错就是一笔错账），
// 也不做四舍五入（"12.345" 是三位小数，用户要么想要 12.34 要么想要 12.35，
// 替他选一个都会静默改掉他填的数）。
func ParseYuanToCents(raw string) (cents int64, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	if strings.HasPrefix(s, "-") {
		return 0, ErrShippingFeeNegative
	}
	intPart, fracPart, hasDot := strings.Cut(s, ".")
	if !isASCIIDigits(intPart) || (hasDot && !isASCIIDigits(fracPart)) {
		return 0, ErrShippingFeeFormat
	}
	if intPart == "" && fracPart == "" {
		return 0, ErrShippingFeeFormat
	}
	if len(fracPart) > 2 {
		return 0, ErrShippingFeeFormat
	}
	yuan := int64(0)
	if intPart != "" {
		parsed, perr := strconv.ParseInt(intPart, 10, 64)
		if perr != nil {
			// 位数超过 int64：这属于「填得太大了」，不是格式错 ——
			// 用户据此能改对，而「格式不合法」会让他盯着自己填的数字看半天。
			return 0, ErrShippingFeeTooLarge
		}
		yuan = parsed
	}
	// **先比上限再乘**：yuan 接近 int64 上限时 yuan*100 会溢出成负数，
	// 而一个负的「分」往下走就是一笔倒贴钱的运费。
	if yuan > MaxShippingFeeCents/100 {
		return 0, ErrShippingFeeTooLarge
	}
	frac := fracPart
	for len(frac) < 2 {
		frac += "0"
	}
	fracCents, ferr := strconv.ParseInt(frac, 10, 64)
	if ferr != nil {
		return 0, ErrShippingFeeFormat
	}
	cents = yuan*100 + fracCents
	if cents > MaxShippingFeeCents {
		return 0, ErrShippingFeeTooLarge
	}
	return cents, nil
}

// FormatCentsAsYuan 分 → 表单里的元（0 显示为空串 = 未配置）。
//
// 与 ParseYuanToCents 同一套整数拆分：分 → 元再回填，必须原样回到用户上次看到的值，
// 否则「保存一次、数字变一次」的漂移会让人怀疑自己有没有改错东西。
func FormatCentsAsYuan(cents int64) string {
	if cents <= 0 {
		return ""
	}
	return strconv.FormatInt(cents/100, 10) + "." + fmt.Sprintf("%02d", cents%100)
}

// isASCIIDigits 全是 ASCII 数字（空串视为合法，由调用方按位置判断是否允许为空）。
//
// 刻意不用 strconv 探位：`ParseInt("１２")`（全角）会失败，而 `ParseInt("+1")`
// 会成功 —— 后者是我们明确拒绝的写法（表单里没有「正号」这回事）。
func isASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
