package orderhttp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"

	"go_wp/internal/web/shell"
)

// coupon_page_view.go - 优惠码管理页的视图构造（列表/编辑/核销行、状态徽章与有效期文案）。

// couponRowView 优惠码行 → 模板视图（展示文本、编辑链接、停用表单的隐藏域都在这里定型）。
func couponRowView(cp *orderdto.CouponResp, filter couponFilter, projectID string, page, limit int) gin.H {
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
		"DiscountLabel":    cp.DiscountLabel,
		"MinSubtotalLabel": cp.MinSubtotalLabel,
		"UsageLabel":       couponUsageLabel(cp.UsedCount, cp.MaxUses),
		"PerUserLabel":     couponNumberLabel(cp.PerUserLimit),
		"WindowLabel":      couponWindowLabel(cp.StartsAt.TimePtr(), cp.EndsAt.TimePtr()),
		"StatusLabel":      cp.StatusLabel,
		"Badge":            couponStatusBadge(cp.StatusLabel),
		"Remark":           orderTextOrEmpty(cp.Remark),
		"EditFormURL":      shell.FilterBaseURL("/admin/coupons/edit-form", editVals),
		"EditURL":          shell.FilterBaseURL("/admin/coupons", vals),
		"CollapseURL":      shell.FilterBaseURL("/admin/coupons", collapse),
		"Expanded":         filter.CouponID == cp.ID,
		// 停用 / 启用复用更新接口，表单必须回送**全部可改字段**（见 CouponUpdate 的注释）。
		"ToggleStatus": strconv.Itoa(couponToggleStatus(cp.Status)),
		"ToggleLabel":  couponToggleLabel(cp.Status),
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
func couponEditView(cp *orderdto.CouponResp, filter couponFilter, projectID string, page, limit int) gin.H {
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
		"StatusLabel":      cp.StatusLabel,
		"Badge":            couponStatusBadge(cp.StatusLabel),
		"DiscountLabel":    cp.DiscountLabel,
		"MinSubtotalLabel": cp.MinSubtotalLabel,
		"UsageLabel":       couponUsageLabel(cp.UsedCount, cp.MaxUses),
		"PerUserLabel":     couponNumberLabel(cp.PerUserLimit),
		"WindowLabel":      couponWindowLabel(cp.StartsAt.TimePtr(), cp.EndsAt.TimePtr()),
		"Remark":           cp.Remark,
		"Back":             couponBackQuery(projectID, filter, page, limit, filter.CouponID),
	}
}

// couponRedemptionRow 核销记录行 → 模板视图。
func couponRedemptionRow(rd *orderdto.CouponRedemptionResp) gin.H {
	if rd == nil {
		return gin.H{}
	}
	// 匿名下单（结算链路不要求先注册）没有 userId：显示「匿名访客」而不是空单元格，
	// 否则看起来像是「核销人丢了」。
	user := "匿名访客"
	if rd.UserID != nil && *rd.UserID > 0 {
		user = strconv.FormatUint(*rd.UserID, 10)
	}
	return gin.H{
		"Time":          orderTimeLabel(rd.CreateTime.Time()),
		"OrderNo":       orderTextOrEmpty(rd.OrderNo),
		"UserID":        user,
		"DiscountLabel": rd.DiscountLabel,
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

// couponStatusBadge 状态文案 → 徽章样式；认不出的状态给中性徽章（不猜颜色）。
func couponStatusBadge(statusLabel string) string {
	if badge, ok := couponStatusBadges[strings.TrimSpace(statusLabel)]; ok {
		return badge
	}
	return "badge-mute"
}

// couponUsageLabel 用次展示：「3 / 100」「3 / 不限」。
func couponUsageLabel(used, max int) string {
	if max <= 0 {
		return fmt.Sprintf("%d / %s", used, couponUnlimitedLabel)
	}
	return fmt.Sprintf("%d / %d", used, max)
}

// couponNumberLabel 次数类字段展示：0 = 不限（每人限次与总上限同口径）。
func couponNumberLabel(limit int) string {
	if limit <= 0 {
		return couponUnlimitedLabel
	}
	return strconv.Itoa(limit)
}

// couponWindowLabel 时间窗展示：两端都不限时只说一次「不限」，不做「不限 ~ 不限」这种噪音。
func couponWindowLabel(startsAt, endsAt *time.Time) string {
	if startsAt == nil && endsAt == nil {
		return couponUnlimitedLabel
	}
	return couponWindowSide(startsAt) + " ~ " + couponWindowSide(endsAt)
}

// couponWindowSide 时间窗的一端：nil = 不限（不是「没有值」——
// 这里「不限」说明该侧没有约束，「—」会让人以为数据缺了）。
func couponWindowSide(at *time.Time) string {
	if at == nil {
		return couponUnlimitedLabel
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
func couponToggleLabel(status int) string {
	if status == 1 {
		return "停用"
	}
	return "启用"
}
