package orderhttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
)

// returnBulkApprove 是勾选多条退货的批量通过。
func (h *Handle) returnBulkApprove(c *gin.Context) {
	runOrderBulkWrite(c,
		func(f *orderdto.ReturnReviewForm) []string { return f.ReturnIDs },
		returnApproveReqs, h.svc.ApproveReturnBatch, "批量同意退货申请失败")
}

func returnApproveReqs(c *gin.Context, form *orderdto.ReturnReviewForm,
	ids []string) ([]*orderdto.ReturnReviewReq, bool) {
	operatorID, operatorName := operatorFromContext(c)
	return returnReviewReqs(ids, form.Remark, operatorID, operatorName), true
}

// returnBulkReject 是勾选多条退货的批量驳回：备注即驳回原因，必填。
func (h *Handle) returnBulkReject(c *gin.Context) {
	runOrderBulkWrite(c,
		func(f *orderdto.ReturnReviewForm) []string { return f.ReturnIDs },
		returnRejectReqs, h.svc.RejectReturnBatch, "批量拒绝退货申请失败")
}

func returnRejectReqs(c *gin.Context, form *orderdto.ReturnReviewForm,
	ids []string) ([]*orderdto.ReturnReviewReq, bool) {
	remark := strings.TrimSpace(form.Remark)
	if remark == "" {
		orderWriteBadInput(c, orderenums.BulkReturnRejectReasonRequired)
		return nil, false
	}
	operatorID, operatorName := operatorFromContext(c)
	return returnReviewReqs(ids, remark, operatorID, operatorName), true
}

func returnReviewReqs(ids []string, remark string, operatorID uint64,
	operatorName string) []*orderdto.ReturnReviewReq {
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
	return reqs
}
