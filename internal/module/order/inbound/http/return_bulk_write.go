package orderhttp

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

var (
	returnBulkApproveKeys = map[string]bool{"ids": true, "remark": true}
	returnBulkRejectKeys  = map[string]bool{"ids": true, "remark": true}
)

func (h *Handle) returnBulkApprove(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteBulkShapeOK(c, raw, returnBulkApproveKeys)
	if !ok {
		return
	}
	remark, ok := orderWriteStringOf(raw, "remark")
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	if blocked := orderBulkShapeBlocked(c, ids); len(blocked) > 0 {
		orderBulkReject(c, blocked)
		return
	}

	reqs := make([]*orderdto.ReturnReviewReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.ReturnReviewReq{
			ReturnID: id,
			Remark:   remark,

			AutoReceive: false,

			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	if err := h.svc.ApproveReturnBatch(ctx, reqs); err != nil {
		logger.Scene(orderWriteScene).Error(err, "批量同意退货申请失败")
		orderBulkReject(c, orderBulkReasonBlocked(ids[0], orderReasonText(c, err)))
		return
	}
	response.Success(c, orderWriteBulkResult{Updated: len(ids)})
}

func (h *Handle) returnBulkReject(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteBulkShapeOK(c, raw, returnBulkRejectKeys)
	if !ok {
		return
	}
	remark, ok := orderWriteStringOf(raw, "remark")
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	if remark == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.BulkReturnRejectReasonRequired)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	if blocked := orderBulkShapeBlocked(c, ids); len(blocked) > 0 {
		orderBulkReject(c, blocked)
		return
	}
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
		logger.Scene(orderWriteScene).Error(err, "批量拒绝退货申请失败")
		orderBulkReject(c, orderBulkReasonBlocked(ids[0], orderReasonText(c, err)))
		return
	}
	response.Success(c, orderWriteBulkResult{Updated: len(ids)})
}
