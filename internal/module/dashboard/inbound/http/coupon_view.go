package dashboardhttp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
)

// coupon_view.go - 优惠码管理页的视图构造（列表/编辑/核销行、状态徽章与有效期文案）。

// couponRowView 优惠码行 → 模板视图（展示文本、编辑链接、停用表单的隐藏域都在这里定型）。
func couponRowView(cp *orderdto.CouponResp, filter couponFilter, projectID string, page, limit int) gin.H {
	if cp == nil {
		return gin.H{}
	}
	// 编辑链接在同一页面上展开（靠 couponId 参数），因此把当前窗口一起带上：
	// 收起编辑区或再翻页时，用户还站在原来那一屏。
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
		"EditURL":          filterBaseURL("/admin/coupons", vals),
		"CollapseURL":      filterBaseURL("/admin/coupons", collapse),
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

// couponFilterValues 列表页链接要保留的筛选条件（空值由 filterBaseURL 丢弃）。
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

// couponFormTime 时间 → 表单回填文本（空串 = 不限）。
//
// 用「2006-01-02 15:04」这一种写法而不是 <input type="datetime-local">：
// 后者提交的是带 T 的 ISO 文本，而服务端只接受 2006-01-02 / 2006-01-02 15:04(:05)，
// 带 T 的写法会被判成「生效时间不合法」—— 也就是表单自己生成的格式自己都不收。
func couponFormTime(at *time.Time) string {
	if at == nil {
		return ""
	}
	return at.Local().Format("2006-01-02 15:04")
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
