package orderhttp

import (
	"context"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

var (
	orderStatusBulkKeys = map[string]bool{"ids": true, "toStatus": true, "remark": true}
	orderCancelBulkKeys = map[string]bool{"ids": true, "remark": true}
)

// runOrderBulkWrite 是批量写端点的统一骨架：绑定 → 归一 → 投影 → 调用 → 出口。
// 归一（去重 / 上限）、投影里的人身份与业务校验、失败渲染都已在下面写出口，
// 各批量端点只剩一句调用。
func runOrderBulkWrite[F, T any](c *gin.Context,
	idsOf func(*F) []string,
	build func(*gin.Context, *F, []string) ([]*T, bool),
	call func(context.Context, []*T) error,
	message string) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[F](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	ids, ok := orderNormalizeBulkIDs(c, idsOf(form))
	if !ok {
		return
	}
	reqs, ok := build(c, form, ids)
	if !ok {
		return
	}
	if err := call(c.Request.Context(), reqs); err != nil {
		orderBulkFailure(c, err, message, ids[0])
		return
	}
	pageview.Success(c)
}

// orderStatusBulk 是勾选多张订单的批量流转。
func (h *Handle) orderStatusBulk(c *gin.Context) {
	runOrderBulkWrite(c,
		func(f *orderdto.OrderStatusForm) []string { return f.OrderIDs },
		orderStatusBulkReqs,
		// 用闭包而不是方法值：方法值在参数求值时就解引用 svc，
		// 会把「形状校验前不碰 service」的契约变成空指针。
		func(ctx context.Context, reqs []*orderdto.ChangeStatusReq) error {
			return h.svc.ChangeStatusBatch(ctx, reqs)
		}, "批量流转订单失败")
}

func orderStatusBulkReqs(c *gin.Context, form *orderdto.OrderStatusForm,
	ids []string) ([]*orderdto.ChangeStatusReq, bool) {
	to := strings.TrimSpace(form.ToStatus)
	if to == "" {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return nil, false
	}
	operatorID, operatorName := operatorFromContext(c)
	return orderChangeStatusReqs(ids, to, form.Remark, operatorID, operatorName), true
}

func orderChangeStatusReqs(ids []string, to, remark string, operatorID uint64,
	operatorName string) []*orderdto.ChangeStatusReq {
	reqs := make([]*orderdto.ChangeStatusReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.ChangeStatusReq{
			OrderID:  id,
			ToStatus: to,
			Remark:   remark,

			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	return reqs
}

// orderCancelBulk 是勾选多张订单的批量取消：备注即取消原因。
func (h *Handle) orderCancelBulk(c *gin.Context) {
	runOrderBulkWrite(c,
		func(f *orderdto.OrderCancelForm) []string { return f.OrderIDs },
		orderCancelBulkReqs,
		func(ctx context.Context, reqs []*orderdto.CancelOrderReq) error {
			return h.svc.CancelOrderBatch(ctx, reqs)
		}, "批量取消订单失败")
}

func orderCancelBulkReqs(c *gin.Context, form *orderdto.OrderCancelForm,
	ids []string) ([]*orderdto.CancelOrderReq, bool) {
	// 批量取消的备注框就叫 remark；单条取消叫 reason。两者都接受，优先 remark。
	reason := strings.TrimSpace(form.Remark)
	if reason == "" {
		reason = strings.TrimSpace(form.Reason)
	}
	if reason == "" {
		orderWriteBadInput(c, orderenums.BulkCancelReasonRequired)
		return nil, false
	}
	operatorID, operatorName := operatorFromContext(c)
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
	return reqs, true
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
