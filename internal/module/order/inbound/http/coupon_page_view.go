package orderhttp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"

	"go_wp/internal/web/shell"
)

// coupon_page_view.go - 优惠码管理页的视图构造（列表/编辑/核销行、状态徽章与有效期文案）。

// 券页的展示标签（key + 中文兜底；**按口径值与原始金额在展示层拼**，不用 service 的
// DiscountLabel / MinSubtotalLabel —— 那是固定中文的 API 字段，页面直接渲染它等于英文界面恒中文）。
var (
	couponDiscountPercentLabel = orderLabel{"admin.coupons.discount.percent", "减 {value}%"}
	couponDiscountFixedLabel   = orderLabel{"admin.coupons.discount.fixed", "减 {value} 元"}
	// couponYuanLabel 「<金额> 元」的展示（使用门槛与核销金额共用：同一个「元」后缀
	// 两处各写一条词条，翻译改动时必然分叉）。
	couponYuanLabel          = orderLabel{"admin.coupons.discount.yuan", "{value} 元"}
	couponAnonymousUserLabel = orderLabel{"admin.coupons.redemption.anonymous", "匿名访客"}
)

// couponStateText 券口径状态 → 展示文案（key + 中文兜底；未知值原样回显）。
func couponStateText(tr translate, state string) string {
	key, fallback := orderenums.CouponStateLabel(state)
	if key == "" {
		return fallback
	}
	return orderLabelOf(tr, orderLabel{key, fallback})
}

// couponDiscountText 券的折扣口径展示（「减 20%」「减 20.00 元」）。
//
// 占位符是 `{value}`，用命名替换（orderLabelFilled）而非 Sprintf：词条里的裸 `%`
// （「减 20%」就有）在 Sprintf 下会被当成格式化动词，输出乱码 —— 而词条能在后台被运营改。
func couponDiscountText(tr translate, discountType string, value int64) string {
	if strings.TrimSpace(discountType) == "percent" {
		return orderLabelFilled(tr, couponDiscountPercentLabel,
			map[string]string{"value": strconv.FormatInt(value, 10)})
	}
	return orderLabelFilled(tr, couponDiscountFixedLabel,
		map[string]string{"value": orderAmountText(value)})
}

// couponYuanText 金额（分）→ 「<元> 元」的展示（使用门槛与核销金额共用）。
func couponYuanText(tr translate, cents int64) string {
	return orderLabelFilled(tr, couponYuanLabel, map[string]string{"value": orderAmountText(cents)})
}

// couponRowView 优惠码行 → 模板视图（展示文本、编辑链接、停用表单的隐藏域都在这里定型）。
func couponRowView(tr translate, cp *orderdto.CouponResp, filter couponFilter, projectID string, page, limit int) gin.H {
	if cp == nil {
		return gin.H{}
	}
	// 编辑抽屉按需取数；核销记录链接仍在同页展开。
	editVals := couponFilterValues(projectID, filter)
	editVals["id"] = strconv.FormatUint(cp.ID, 10)
	editVals["page"] = strconv.Itoa(page)
	editVals["limit"] = strconv.Itoa(limit)
	vals := couponFilterValues(projectID, filter)
	vals["couponId"] = strconv.FormatUint(cp.ID, 10)
	vals["page"] = strconv.Itoa(page)
	vals["limit"] = strconv.Itoa(limit)
	collapse := couponFilterValues(projectID, filter)
	collapse["page"] = strconv.Itoa(page)
	collapse["limit"] = strconv.Itoa(limit)

	return gin.H{
		"Code":             cp.Code,
		"Name":             orderTextOrEmpty(cp.Name),
		"DiscountLabel":    couponDiscountText(tr, cp.DiscountType, cp.DiscountValue),
		"MinSubtotalLabel": couponYuanText(tr, cp.MinSubtotal),
		"UsageLabel":       couponUsageLabel(tr, cp.UsedCount, cp.MaxUses),
		"PerUserLabel":     couponNumberLabel(tr, cp.PerUserLimit),
		"WindowLabel":      couponWindowLabel(tr, cp.StartsAt.TimePtr(), cp.EndsAt.TimePtr()),
		"StatusLabel":      couponStateText(tr, cp.State),
		"Badge":            couponStatusBadge(cp.State),
		"Remark":           orderTextOrEmpty(cp.Remark),
		"EditFormURL":      shell.FilterBaseURL("/admin/coupons/edit-form", editVals),
		"EditURL":          shell.FilterBaseURL("/admin/coupons", vals),
		"CollapseURL":      shell.FilterBaseURL("/admin/coupons", collapse),
		"Expanded":         filter.CouponID == cp.ID,
		// 停用 / 启用复用更新接口，表单必须回送**全部可改字段**（见 CouponUpdate 的注释）。
		"ToggleStatus": strconv.Itoa(couponToggleStatus(cp.Status)),
		"ToggleLabel":  couponToggleLabel(tr, cp.Status),
		"Form": gin.H{
			"ID":            strconv.FormatUint(cp.ID, 10),
			"Name":          cp.Name,
			"DiscountType":  cp.DiscountType,
			"DiscountValue": cp.DiscountValue,
			"MinSubtotal":   cp.MinSubtotal,
			"MaxUses":       cp.MaxUses,
			"PerUserLimit":  cp.PerUserLimit,
			"StartsAt":      couponFormTime(cp.StartsAt.TimePtr()),
			"EndsAt":        couponFormTime(cp.EndsAt.TimePtr()),
			"Remark":        cp.Remark,
			// 编辑抽屉要回显当前状态（ToggleStatus 是「切换后」的目标值，不能拿来当初始值，
			// 否则打开抽屉随手保存就会把券的状态反转）。
			"StatusValue": strconv.Itoa(cp.Status),
		},
		"Back": couponBackQuery(projectID, filter, page, limit, filter.CouponID),
	}
}

// couponEditView 展开区（编辑表单 + 该券的当前状态摘要）→ 模板视图。
func couponEditView(tr translate, cp *orderdto.CouponResp, filter couponFilter, projectID string, page, limit int) gin.H {
	if cp == nil {
		return gin.H{}
	}
	return gin.H{
		"ID":               cp.ID,
		"Code":             cp.Code,
		"Name":             cp.Name,
		"DiscountType":     cp.DiscountType,
		"DiscountValue":    cp.DiscountValue,
		"MinSubtotal":      cp.MinSubtotal,
		"MaxUses":          cp.MaxUses,
		"PerUserLimit":     cp.PerUserLimit,
		"StartsAt":         couponFormTime(cp.StartsAt.TimePtr()),
		"EndsAt":           couponFormTime(cp.EndsAt.TimePtr()),
		"StatusValue":      strconv.Itoa(cp.Status),
		"StatusLabel":      couponStateText(tr, cp.State),
		"Badge":            couponStatusBadge(cp.State),
		"DiscountLabel":    couponDiscountText(tr, cp.DiscountType, cp.DiscountValue),
		"MinSubtotalLabel": couponYuanText(tr, cp.MinSubtotal),
		"UsageLabel":       couponUsageLabel(tr, cp.UsedCount, cp.MaxUses),
		"PerUserLabel":     couponNumberLabel(tr, cp.PerUserLimit),
		"WindowLabel":      couponWindowLabel(tr, cp.StartsAt.TimePtr(), cp.EndsAt.TimePtr()),
		"Remark":           cp.Remark,
		"Back":             couponBackQuery(projectID, filter, page, limit, filter.CouponID),
	}
}

// couponRedemptionRow 核销记录行 → 模板视图。
func couponRedemptionRow(tr translate, rd *orderdto.CouponRedemptionResp) gin.H {
	if rd == nil {
		return gin.H{}
	}
	// 匿名下单（结算链路不要求先注册）没有 userId：显示「匿名访客」而不是空单元格，
	// 否则看起来像是「核销人丢了」。
	user := orderLabelOf(tr, couponAnonymousUserLabel)
	if rd.UserID != nil && *rd.UserID > 0 {
		user = strconv.FormatUint(*rd.UserID, 10)
	}
	return gin.H{
		"Time":          orderTimeLabel(rd.CreateTime.Time()),
		"OrderNo":       orderTextOrEmpty(rd.OrderNo),
		"UserID":        user,
		"DiscountLabel": couponYuanText(tr, rd.DiscountAmount),
	}
}

// couponFilterValues 列表页链接要保留的筛选条件（空值由 shell.FilterBaseURL 丢弃）。
func couponFilterValues(projectID string, filter couponFilter) map[string]string {
	return map[string]string{
		"project": projectID,
		"status":  filter.Status,
		"keyword": filter.Keyword,
	}
}

// couponStatusBadge 状态**口径值** → 徽章样式；认不出的状态给中性徽章（不猜颜色）。
//
// 判据是口径值（cp.State），不是展示文案：文案会随词条与语言变，拿它当键等于
// 「后台改一句词条 → 徽章静默变灰」。
func couponStatusBadge(state string) string {
	if badge, ok := couponStatusBadges[strings.TrimSpace(state)]; ok {
		return badge
	}
	return "badge-mute"
}

// couponUsageLabel 用次展示：「3 / 100」「3 / 不限」。
func couponUsageLabel(tr translate, used, max int) string {
	if max <= 0 {
		return fmt.Sprintf("%d / %s", used, orderLabelOf(tr, couponUnlimitedLabel))
	}
	return fmt.Sprintf("%d / %d", used, max)
}

// couponNumberLabel 次数类字段展示：0 = 不限（每人限次与总上限同口径）。
func couponNumberLabel(tr translate, limit int) string {
	if limit <= 0 {
		return orderLabelOf(tr, couponUnlimitedLabel)
	}
	return strconv.Itoa(limit)
}

// couponWindowLabel 时间窗展示：两端都不限时只说一次「不限」，不做「不限 ~ 不限」这种噪音。
func couponWindowLabel(tr translate, startsAt, endsAt *time.Time) string {
	if startsAt == nil && endsAt == nil {
		return orderLabelOf(tr, couponUnlimitedLabel)
	}
	return couponWindowSide(tr, startsAt) + " ~ " + couponWindowSide(tr, endsAt)
}

// couponWindowSide 时间窗的一端：nil = 不限（不是「没有值」——
// 这里「不限」说明该侧没有约束，「—」会让人以为数据缺了）。
func couponWindowSide(tr translate, at *time.Time) string {
	if at == nil {
		return orderLabelOf(tr, couponUnlimitedLabel)
	}
	return orderTimeLabel(*at)
}

// couponFormTime 时间 → 表单回填值（空串 = 不限）。
//
// 输出带 T 的形态（`2006-01-02T15:04`，秒非 0 时到秒）—— 这是 HTML 规范给
// <input type="datetime-local"> 规定的 value 写法（空格形态不在规范里：Chrome 实测会顺手
// 规范化，但那是实现宽容，不能依赖；纯日期形态则会被直接清成空串，所以回填必须含时刻）。
//
// 秒与亚秒的取舍：秒非 0 时输出到秒（模板对应输入框带 step="1"，否则控件会以
// stepMismatch 判非法、**表单根本提交不了**）；亚秒一律丢弃 —— datetime-local 无法安全表达
// 微秒，而券的生效窗口精确到秒已经过头了。
//
// 提交回来的是同一种写法，由 couponFormTimeValue（coupon_page_query.go）归一化成服务端的
// 解析布局 —— 「控件格式 ↔ 服务端口径」之间的转换只有那一处，且只有一份。
func couponFormTime(at *time.Time) string {
	if at == nil {
		return ""
	}
	local := at.Local()
	if local.Second() != 0 {
		return local.Format("2006-01-02T15:04:05")
	}
	return local.Format("2006-01-02T15:04")
}

// couponToggleStatus 停用 / 启用目标值：生效的券给出停用（0），其余给出启用（1）。
func couponToggleStatus(status int) int {
	if status == 1 {
		return 0
	}
	return 1
}

// couponToggleLabel 停用 / 启用按钮文案。
func couponToggleLabel(tr translate, status int) string {
	if status == 1 {
		return orderLabelOf(tr, couponFormDisabledLabel)
	}
	return orderLabelOf(tr, couponFormEnabledLabel)
}
