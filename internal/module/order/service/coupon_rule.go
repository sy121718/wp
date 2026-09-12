package orderservice

// coupon_rule.go — 优惠码的规则判定与金额试算（BIZ-1）。
//
// 「能不能用」与「能减多少」两件事都收在这一个文件里：试算（结算页）与核销（下单事务）
// 必须用同一份逻辑，否则会出现「结算页说能减 20、下单却只减 10」这种只能靠客诉发现的问题。

import (
	"errors"
	"fmt"
	"strings"
	"time"

	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

const (
	// maxCouponCodeLen 券码长度上限（与数据库列宽一致）。
	maxCouponCodeLen = 64
	// maxCouponNameLen 优惠码名称长度上限。
	maxCouponNameLen = 120
	// maxCouponRemarkLen 备注长度上限。
	maxCouponRemarkLen = 255
)

// normalizeCouponCode 券码归一化：去首尾空白 + 转大写。
//
// 只在这一处发生。两处各转一次，迟早在某条路径上漏掉一次，
// 表现是「客服抄给客户的码用不了」—— 而客户只会认为这张券是假的。
func normalizeCouponCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// couponTimeLayouts 后台表单与接口可能送来的时间写法。
//
// 只接受不带时区的本地时间：优惠活动按站点运营的本地时间理解。
// 接受带时区的 RFC3339 会引出一个「按谁的时区算」的问题，
// 而这个问题在跨境场景下没有正确答案。
var couponTimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// parseCouponTime 解析时间窗文本；空串 = 不限（nil）。
func parseCouponTime(raw string) (*time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	for _, layout := range couponTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return &t, nil
		}
	}
	return nil, errors.New(orderenums.ErrCouponWindowInvalid)
}

// validateCouponRule 校验优惠码字段（新建与修改共用）。
//
// 逐条拒绝而不是静默纠正：把 percent=150 悄悄改成 100，运营看到的是「保存成功了」，
// 而券的力度与他填的不是一回事。
func validateCouponRule(e *ordermodel.CouponEntity) error {
	if strings.TrimSpace(e.ProjectID) == "" {
		return errors.New(orderenums.ErrProjectRequired)
	}
	if e.Code == "" {
		return errors.New(orderenums.ErrCouponCodeRequired)
	}
	if len(e.Code) > maxCouponCodeLen || len(e.Name) > maxCouponNameLen || len(e.Remark) > maxCouponRemarkLen {
		return errors.New(orderenums.ErrInvalidParam)
	}
	switch e.DiscountType {
	case ordermodel.CouponTypePercent:
		if e.DiscountValue <= 0 || e.DiscountValue > 100 {
			return errors.New(orderenums.ErrCouponValueInvalid)
		}
	case ordermodel.CouponTypeFixed:
		if e.DiscountValue <= 0 {
			return errors.New(orderenums.ErrCouponValueInvalid)
		}
	default:
		return errors.New(orderenums.ErrCouponTypeInvalid)
	}
	if e.MinSubtotal < 0 {
		return errors.New(orderenums.ErrCouponValueInvalid)
	}
	if e.MaxUses < 0 || e.PerUserLimit < 0 {
		return errors.New(orderenums.ErrCouponValueInvalid)
	}
	if e.StartsAt != nil && e.EndsAt != nil && !e.EndsAt.After(*e.StartsAt) {
		// 结束不晚于开始 = 这张券永远不可能生效，存进去等于把活动悄悄废掉。
		return errors.New(orderenums.ErrCouponWindowInvalid)
	}
	if e.Status != ordermodel.CouponStatusEnabled && e.Status != ordermodel.CouponStatusDisabled {
		return errors.New(orderenums.ErrInvalidParam)
	}
	return nil
}

// couponDiscount 折扣额（分）。纯函数：只依赖券口径与小计。
//
// percent 的语义是**折扣力度**（减去小计的百分之多少，100 = 全免），不是折后价：
// 「打 8 折」与「减 80%」差着一个数量级，这类歧义必须在注释里钉死，
// 否则下一个人照着字段名 discountValue 猜，就会猜出另一套算法。
//
// 百分比按分向下取整（不足 1 分不减免）：向上取整会让商家在每单上多让一点利。
func couponDiscount(e *ordermodel.CouponEntity, subtotal int64) int64 {
	if e == nil || subtotal <= 0 {
		return 0
	}
	var d int64
	switch e.DiscountType {
	case ordermodel.CouponTypePercent:
		d = subtotal * e.DiscountValue / 100
	case ordermodel.CouponTypeFixed:
		d = e.DiscountValue
	default:
		return 0
	}
	if d > subtotal {
		// 折扣不得超过小计：负数总额没有意义，而且会把账算乱。
		d = subtotal
	}
	if d < 0 {
		d = 0
	}
	return d
}

// couponRuleCheck 券自身状态的可用性检查，返回不可用原因（空串 = 通过）。
//
// 这里刻意**不含**每人限次：那条要查核销表，不是纯计算，
// 混进来会让「试算」这个纯读函数偷偷变成一次数据库往返的隐式依赖。
func couponRuleCheck(e *ordermodel.CouponEntity, subtotal int64, now time.Time) string {
	if e == nil {
		return orderenums.ErrCouponNotFound
	}
	switch {
	case e.Status != ordermodel.CouponStatusEnabled:
		return orderenums.ErrCouponDisabled
	case e.StartsAt != nil && now.Before(*e.StartsAt):
		return orderenums.ErrCouponNotStarted
	case e.EndsAt != nil && now.After(*e.EndsAt):
		return orderenums.ErrCouponExpired
	case e.MaxUses > 0 && e.UsedCount >= e.MaxUses:
		return orderenums.ErrCouponExhausted
	case subtotal < e.MinSubtotal:
		return orderenums.ErrCouponMinSubtotal
	}
	return ""
}

// couponDiscountLabel 券口径的展示文案（如「减 20%」「减 20.00 元」）。
func couponDiscountLabel(e *ordermodel.CouponEntity) string {
	if e == nil {
		return ""
	}
	if e.DiscountType == ordermodel.CouponTypePercent {
		return fmt.Sprintf("减 %d%%", e.DiscountValue)
	}
	return fmt.Sprintf("减 %s 元", centsToYuanLabel(e.DiscountValue))
}

// couponStatusLabel 券当前状态的展示文案（按时间与次数算出来，不看单一列）。
//
// 状态列只表达「运营有没有手动停用」；过期、未开始、用尽都是时间的函数，
// 单独存一列必然与真实状态不同步（要靠定时任务去刷，而定时任务总有停的时候）。
func couponStatusLabel(e *ordermodel.CouponEntity, now time.Time) string {
	if e == nil {
		return ""
	}
	switch {
	case e.Status != ordermodel.CouponStatusEnabled:
		return "已停用"
	case e.EndsAt != nil && now.After(*e.EndsAt):
		return "已过期"
	case e.StartsAt != nil && now.Before(*e.StartsAt):
		return "未开始"
	case e.MaxUses > 0 && e.UsedCount >= e.MaxUses:
		return "已用完"
	}
	return "生效中"
}

// usageLabel 用次展示：「3 / 不限」「3 / 100」。
func usageLabel(used, max int) string {
	if max <= 0 {
		return fmt.Sprintf("%d / 不限", used)
	}
	return fmt.Sprintf("%d / %d", used, max)
}
