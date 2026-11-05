package orderhttp

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/utils"
)

// couponTimeInputValue 把浏览器 datetime-local 的值规整成数据库接受的时刻串。
func couponTimeInputValue(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return s
}

var couponReasonFallbacks = map[string]string{
	orderenums.ErrCouponCodeTaken:     "这个优惠码已经存在",
	orderenums.ErrCouponTypeInvalid:   "优惠类型只支持按比例折扣或固定金额",
	orderenums.ErrCouponValueInvalid:  "优惠值不合法",
	orderenums.ErrCouponWindowInvalid: "生效时间不合法",
	orderenums.ErrCouponInUse:         "该优惠码已有核销记录，不能删除",
}

// orderCouponReasonText 取一条优惠码批量逐条失败的原因（按请求语言）。
func orderCouponReasonText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	msg := couponFacingText(err.Error())
	if msg == "" {
		return orderFacingFallback(c, err)
	}
	if text := orderWriteTranslation(c, msg); text != msg {
		return text
	}
	if fallback := couponReasonFallbacks[msg]; fallback != "" {
		return fallback
	}
	return msg
}

func couponTimeValueUTC(v *utils.JSONTime) string {
	if v == nil {
		return ""
	}
	return v.Time().UTC().Format("2006-01-02 15:04:05")
}

func couponToggleReq(cp *orderdto.CouponResp, target int, operatorID uint64, operatorName string) *orderdto.CouponSaveReq {
	return &orderdto.CouponSaveReq{
		ID:            cp.ID,
		ProjectID:     cp.ProjectID,
		Code:          cp.Code,
		Name:          cp.Name,
		DiscountType:  cp.DiscountType,
		DiscountValue: cp.DiscountValue,
		MinSubtotal:   cp.MinSubtotal,
		MaxUses:       cp.MaxUses,
		PerUserLimit:  cp.PerUserLimit,
		StartsAt:      couponTimeValueUTC(cp.StartsAt),
		EndsAt:        couponTimeValueUTC(cp.EndsAt),
		Status:        target,
		Remark:        cp.Remark,

		OperatorID:   operatorID,
		OperatorName: operatorName,
	}
}

// couponDeleteBulk 是勾选多张优惠码的批量删除。
func (h *Handle) couponDeleteBulk(c *gin.Context) {
	var form orderdto.CouponDeleteForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderNormalizeBulkIDs(c, form.IDs)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	rows := make([]orderBulkResultRow, 0, len(ids))
	done, skipped := 0, 0
	for _, rawID := range ids {
		id, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || id == 0:
			rows = append(rows, orderBulkResultRow{ID: rawID, Message: orderWriteTranslation(c, orderenums.ErrInvalidParam)})
			skipped++
		default:
			if derr := h.svc.DeleteCoupon(ctx, id); derr != nil {
				orderLogWriteError(c, derr, "批量删除优惠码失败")
				rows = append(rows, orderBulkResultRow{ID: rawID, Message: orderCouponReasonText(c, derr)})
				skipped++
			} else {
				done++
			}
		}
	}
	orderBulkResult(c, done, skipped, rows)
}

// couponToggleBulk 是勾选多张优惠码的批量启停。
func (h *Handle) couponToggleBulk(c *gin.Context) {
	var form orderdto.CouponSaveReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	// 表单里没这个键、或给了空串 = 未提供；有了就必须是 0/1。空串会绑成 0，不能当成合法值。
	raw, present := c.Request.PostForm["status"]
	if !present || len(raw) == 0 || strings.TrimSpace(raw[0]) == "" {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	target, ok := couponToggleTargetText(form.Status)
	if !ok {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderNormalizeBulkIDs(c, form.IDs)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(form.ProjectID)
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()
	rows := make([]orderBulkResultRow, 0, len(ids))
	done, skipped := 0, 0
	for _, rawID := range ids {
		id, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || id == 0:
			rows = append(rows, orderBulkResultRow{ID: rawID, Message: orderWriteTranslation(c, orderenums.ErrInvalidParam)})
			skipped++
		default:
			cp, gerr := h.svc.GetCoupon(ctx, id)
			switch {
			case gerr != nil || cp == nil:
				rows = append(rows, orderBulkResultRow{ID: rawID, Message: orderCouponNotFoundText(c)})
				skipped++
			case projectID != "" && cp.ProjectID != projectID:
				rows = append(rows, orderBulkResultRow{ID: rawID, Message: orderCouponNotFoundText(c)})
				skipped++
			default:
				if _, uerr := h.svc.UpdateCoupon(ctx, couponToggleReq(cp, target, operatorID, operatorName)); uerr != nil {
					orderLogWriteError(c, uerr, "批量启停优惠码失败")
					rows = append(rows, orderBulkResultRow{ID: rawID, Message: orderCouponReasonText(c, uerr)})
					skipped++
				} else {
					done++
				}
			}
		}
	}
	orderBulkResult(c, done, skipped, rows)
}

// couponToggleTargetText 只接受 "0" / "1"：表单取值是字符串，语义是启停。
func couponToggleTargetText(raw int) (int, bool) {
	switch raw {
	case 0, 1:
		return raw, true
	default:
		return 0, false
	}
}

// orderCouponNotFoundText 取「优惠码不存在」的当前语言文案。
func orderCouponNotFoundText(c *gin.Context) string {
	return orderWriteTranslation(c, orderenums.ErrCouponNotFound)
}
