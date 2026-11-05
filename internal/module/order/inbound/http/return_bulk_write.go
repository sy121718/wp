package orderhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/facing"
)

// returnBulkApprove 是勾选多条退货的批量通过。
func (h *Handle) returnBulkApprove(c *gin.Context) {
	var form orderdto.ReturnReviewForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderNormalizeBulkIDs(c, form.ReturnIDs)
	if !ok {
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	reqs := make([]*orderdto.ReturnReviewReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.ReturnReviewReq{
			ReturnID: id,
			Remark:   form.Remark,

			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	if err := h.svc.ApproveReturnBatch(ctx, reqs); err != nil {
		orderLogWriteError(c, err, "批量同意退货申请失败")
		orderBulkFail(c, []facing.BlockedItem{{
			ID:        ids[0],
			ReasonKey: orderenums.ErrBulkRejected,
			Fallback:  orderFacingFallback(c, err),
		}})
		return
	}
	orderBulkResult(c, len(ids), 0, nil)
}

// returnBulkReject 是勾选多条退货的批量驳回：备注即驳回原因，必填。
func (h *Handle) returnBulkReject(c *gin.Context) {
	var form orderdto.ReturnReviewForm
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderNormalizeBulkIDs(c, form.ReturnIDs)
	if !ok {
		return
	}
	remark := strings.TrimSpace(form.Remark)
	if remark == "" {
		orderWriteBadInput(c, orderenums.BulkReturnRejectReasonRequired)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	reqs := make([]*orderdto.ReturnReviewReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.ReturnReviewReq{
			ReturnID:     id,
			Remark:       remark,
			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	if err := h.svc.RejectReturnBatch(ctx, reqs); err != nil {
		orderLogWriteError(c, err, "批量拒绝退货申请失败")
		orderBulkFail(c, []facing.BlockedItem{{
			ID:        ids[0],
			ReasonKey: orderenums.ErrBulkRejected,
			Fallback:  orderFacingFallback(c, err),
		}})
		return
	}
	orderBulkResult(c, len(ids), 0, nil)
}
