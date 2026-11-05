package orderhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

var (
	orderStatusBulkKeys = map[string]bool{"ids": true, "toStatus": true, "remark": true}
	orderCancelBulkKeys = map[string]bool{"ids": true, "remark": true}
)

// orderStatusBulk 是勾选多张订单的批量流转。
func (h *Handle) orderStatusBulk(c *gin.Context) {
	var form orderdto.OrderStatusForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderNormalizeBulkIDs(c, form.OrderIDs)
	if !ok {
		return
	}
	fmtStatus := strings.TrimSpace(form.ToStatus)
	if fmtStatus == "" {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	reqs := make([]*orderdto.ChangeStatusReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.ChangeStatusReq{
			OrderID:  id,
			ToStatus: fmtStatus,
			Remark:   form.Remark,

			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	if err := h.svc.ChangeStatusBatch(ctx, reqs); err != nil {
		orderLogWriteError(c, err, "批量流转订单失败")
		orderBulkFail(c, []facing.BlockedItem{{
			ID:        ids[0],
			ReasonKey: orderenums.ErrBulkRejected,
			Fallback:  orderFacingFallback(c, err),
		}})
		return
	}
	orderBulkResult(c, len(ids), 0, nil)
}

// orderCancelBulk 是勾选多张订单的批量取消：备注即取消原因。
func (h *Handle) orderCancelBulk(c *gin.Context) {
	var form orderdto.OrderCancelForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderNormalizeBulkIDs(c, form.OrderIDs)
	if !ok {
		return
	}
	// 批量取消的备注框就叫 remark；单条取消叫 reason。两者都接受，优先 remark。
	reason := strings.TrimSpace(form.Remark)
	if reason == "" {
		reason = strings.TrimSpace(form.Reason)
	}
	if reason == "" {
		orderWriteBadInput(c, orderenums.BulkCancelReasonRequired)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	reqs := make([]*orderdto.CancelOrderReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.CancelOrderReq{
			OrderID:      id,
			Reason:       reason,
			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	if err := h.svc.CancelOrderBatch(ctx, reqs); err != nil {
		orderLogWriteError(c, err, "批量取消订单失败")
		orderBulkFail(c, []facing.BlockedItem{{
			ID:        ids[0],
			ReasonKey: orderenums.ErrBulkRejected,
			Fallback:  orderFacingFallback(c, err),
		}})
		return
	}
	orderBulkResult(c, len(ids), 0, nil)
}

// orderFacingFallback 取一条业务错误按请求语言取词后的兜底文案；非业务错误统一给内部错误。
func orderFacingFallback(c *gin.Context, err error) string {
	if key := orderFacingText(err.Error()); key != "" {
		if text := response.TranslateMessage(c, key); text != key {
			return text
		}
	}
	return response.TranslateMessage(c, orderenums.ErrInternal)
}
