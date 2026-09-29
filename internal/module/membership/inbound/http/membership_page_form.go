// membership_page_form.go — 后台页面的表单解析与展示格式化（BIZ-3）。
//
// 这一层只做两件事：把运营在页面上的输入翻译成 service 的入参，把 service 的输出翻译成
// 模板直接可渲染的文本。**换算只在这里做一次** —— 门槛在库里是「分」（与 orders 的金额列同口径），
// 而运营的输入与阅读习惯是「元」：这个边界就是本文件，别处不许再有第二次换算
// （两次换算的典型症状是「填 1000 存成 100000」，而两处看起来都合理）。
package membershiphttp

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	membershipenums "go_wp/internal/module/membership/enums"
)

// parseYuanToFen 把「元」输入解析成「分」（整数运算，不经过浮点）。
//
// 不用 strconv.ParseFloat(raw, 64) * 100：0.1 这类二进制无法精确表示的值乘 100 会得到
// 9.999999999999998，四舍五入之后偶尔差 1 分 —— 而门槛差 1 分会把「消费满 1000 元」的会员
// 挡在门外一单。整数拆分（整数部分 + 两位小数）没有这个问题。
//
// 接受形态：`1000` / `1000.5` / `1000.50` / `1,000.50`（千分位容忍，运营从表格里复制常见）；
// 空串返回 (0, nil) —— 调用方据此区分「没填」与「填了 0」。
func parseYuanToFen(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	value = strings.ReplaceAll(value, ",", "")
	if value == "" {
		return 0, nil
	}
	neg := false
	if strings.HasPrefix(value, "-") {
		neg = true
		value = value[1:]
	}
	intPart, fracPart, _ := strings.Cut(value, ".")
	if intPart == "" {
		intPart = "0"
	}
	if len(fracPart) > 2 {
		return 0, errors.New(membershipenums.ErrInvalidParam)
	}
	// 一位小数补零到两位，保证「1.5 元」读作 150 分而不是 15 分。
	for len(fracPart) < 2 {
		fracPart += "0"
	}
	if fracPart == "" {
		fracPart = "00"
	}
	whole, ierr := strconv.ParseInt(intPart, 10, 64)
	if ierr != nil {
		return 0, errors.New(membershipenums.ErrInvalidParam)
	}
	frac, ferr := strconv.ParseInt(fracPart, 10, 64)
	if ferr != nil {
		return 0, errors.New(membershipenums.ErrInvalidParam)
	}
	fen := whole*100 + frac
	if neg {
		fen = -fen
	}
	return fen, nil
}

// formatFenToYuan 把「分」格式化成「元」的两位小数字符串（表单回填与列表展示共用）。
func formatFenToYuan(fen int64) string {
	neg := fen < 0
	if neg {
		fen = -fen
	}
	out := strconv.FormatInt(fen/100, 10) + "." + twoDigits(fen%100)
	if neg {
		return "-" + out
	}
	return out
}

// twoDigits 补足两位（0 → "00"，5 → "05"）。
func twoDigits(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

// formBool 解析表单里的勾选值（HTML checkbox 未勾选时字段根本不存在，值为空串）。
func formBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// formInt 解析整数输入，空串或非法值回落默认值。
func formInt(raw string, fallback int) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// formInt64 解析 64 位整数输入，空串或非法值回落默认值。
func formInt64(raw string, fallback int64) int64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

// parseEntitlementsFromForm 从等级表单里抽出权益清单（新建等级与保存权益共用）。
//
// 表单形态是「两个固定字段」而不是动态行：本期只交付两种权益
// （免运费 / 折扣），固定字段让「没填折扣」与「折扣填 0」能被区分 ——
// 前者是不设折扣，后者（0%）在取值域之外（折扣只接受 1..100）。
func parseEntitlementsFromForm(c *gin.Context) []membershipentitlementForm {
	items := make([]membershipentitlementForm, 0, 2)
	items = append(items, membershipentitlementForm{
		Enabled:  c.PostForm("freeShipping") != "",
		Kind:     membershipenums.KindFreeShipping,
		ValueInt: boolToInt64(formBool(c.PostForm("freeShipping"))),
	})
	discountRaw := strings.TrimSpace(c.PostForm("discountPercent"))
	items = append(items, membershipentitlementForm{
		Enabled:  discountRaw != "",
		Kind:     membershipenums.KindDiscount,
		ValueInt: formInt64(discountRaw, 0),
	})
	return items
}

// membershipentitlementForm 表单里一条权益的原始输入（Enabled = 运营在页面上给了值）。
type membershipentitlementForm struct {
	Enabled  bool
	Kind     string
	ValueInt int64
}

// boolToInt64 布尔 → 免运费的 value_int（0/1）。
func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
