package orderhttp

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
)

var (
	orderCouponDeleteSingleKeys = map[string]bool{"id": true, "couponId": true}
	orderCouponDeleteBulkKeys   = map[string]bool{"ids": true}

	orderCouponToggleBulkKeys = map[string]bool{"ids": true, "status": true, "projectId": true}
)

func orderCouponDeleteIDKey(raw map[string]json.RawMessage) (string, bool) {
	hasID := orderWriteHasKey(raw, "id")
	hasCouponID := orderWriteHasKey(raw, "couponId")
	switch {
	case hasID && hasCouponID:
		return "", false
	case hasCouponID:
		return "couponId", true
	case hasID:
		return "id", true
	default:
		return "", false
	}
}

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

type orderCouponBulkItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted,omitempty"`
	Updated bool   `json:"updated,omitempty"`
	Message string `json:"message,omitempty"`
}

type orderCouponBulkResult struct {
	Deleted int                   `json:"deleted"`
	Updated int                   `json:"updated"`
	Skipped int                   `json:"skipped"`
	Items   []orderCouponBulkItem `json:"items"`
}

var couponReasonFallbacks = map[string]string{
	orderenums.ErrCouponCodeTaken:     "这个优惠码已经存在",
	orderenums.ErrCouponTypeInvalid:   "优惠类型只支持按比例折扣或固定金额",
	orderenums.ErrCouponValueInvalid:  "优惠值不合法",
	orderenums.ErrCouponWindowInvalid: "生效时间不合法",
	orderenums.ErrCouponInUse:         "该优惠码已有核销记录，不能删除",
}

func orderCouponReasonText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	msg := couponFacingText(err.Error())
	if msg == "" {
		return orderReasonText(c, err)
	}
	if text := response.TranslateMessage(c, msg); text != msg {
		return text
	}
	if fallback := couponReasonFallbacks[msg]; fallback != "" {
		return fallback
	}

	return msg
}

func orderWriteMissingCouponText(c *gin.Context) string {
	return orderWriteTextOf(c, orderenums.ErrCouponNotFound, "优惠码不存在")
}

func orderCouponToggleTargetOf(raw map[string]json.RawMessage) (int, bool) {
	body, ok := raw["status"]
	if !ok || orderWriteIsExplicitNull(body) {
		return 0, false
	}
	switch strings.TrimSpace(string(body)) {
	case "0":
		return 0, true
	case "1":
		return 1, true
	default:
		return 0, false
	}
}

func orderCouponProjectOf(raw map[string]json.RawMessage) (string, bool) {
	body, ok := raw["projectId"]
	if !ok {
		return "", true
	}
	if orderWriteIsExplicitNull(body) {
		return "", false
	}
	var value string
	if err := json.Unmarshal(body, &value); err != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
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

func (h *Handle) couponDeleteBulk(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteNormalizeIDs(c, raw)
	if !ok {
		return
	}
	res := orderCouponBulkResult{Items: make([]orderCouponBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := orderCouponBulkItem{ID: rawID}
		id, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || id == 0:

			item.Message = orderWriteParamText(c)
		default:
			if derr := h.svc.DeleteCoupon(ctx, id); derr != nil {
				logger.Scene(orderWriteScene).With("id", rawID).Error(derr, "批量删除优惠码失败")
				item.Message = orderCouponReasonText(c, derr)
			} else {
				item.Deleted = true
			}
		}
		if item.Deleted {
			res.Deleted++
		} else {
			res.Skipped++
		}
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}

func (h *Handle) couponToggleBulk(c *gin.Context, raw map[string]json.RawMessage) {
	target, ok := orderCouponToggleTargetOf(raw)
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	projectID, ok := orderCouponProjectOf(raw)
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderWriteNormalizeIDs(c, raw)
	if !ok {
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	res := orderCouponBulkResult{Items: make([]orderCouponBulkItem, 0, len(ids))}
	ctx := c.Request.Context()
	for _, rawID := range ids {
		item := orderCouponBulkItem{ID: rawID}
		id, perr := strconv.ParseUint(rawID, 10, 64)
		switch {
		case perr != nil || id == 0:
			item.Message = orderWriteParamText(c)
		default:
			cp, gerr := h.svc.GetCoupon(ctx, id)
			switch {
			case gerr != nil || cp == nil:
				item.Message = orderWriteMissingCouponText(c)
			case projectID != "" && cp.ProjectID != projectID:

				item.Message = orderWriteMissingCouponText(c)
			default:
				if _, uerr := h.svc.UpdateCoupon(ctx, couponToggleReq(cp, target, operatorID, operatorName)); uerr != nil {
					logger.Scene(orderWriteScene).With("id", rawID).Error(uerr, "批量启停优惠码失败")
					item.Message = orderCouponReasonText(c, uerr)
				} else {
					item.Updated = true
				}
			}
		}
		if item.Updated {
			res.Updated++
		} else {
			res.Skipped++
		}
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}
