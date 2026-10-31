package orderhttp

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

var (
	orderStatusBulkKeys = map[string]bool{"ids": true, "toStatus": true, "remark": true}
	orderCancelBulkKeys = map[string]bool{"ids": true, "remark": true}
)

type orderWriteBulkItem struct {
	ID      string `json:"id"`
	Updated bool   `json:"updated,omitempty"`
	Message string `json:"message,omitempty"`
}

type orderWriteBulkResult struct {
	Updated int `json:"updated"`
}

func orderWriteStringOf(raw map[string]json.RawMessage, key string) (string, bool) {
	body, ok := raw[key]
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

func orderWriteBulkShapeOK(c *gin.Context, raw map[string]json.RawMessage, allow map[string]bool) ([]string, bool) {
	if !orderWriteShapeOK(raw, allow) {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return nil, false
	}
	return orderWriteNormalizeIDs(c, raw)
}

func (h *Handle) orderStatusBulk(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteBulkShapeOK(c, raw, orderStatusBulkKeys)
	if !ok {
		return
	}
	toStatus, ok := orderWriteStringOf(raw, "toStatus")
	if !ok || strings.TrimSpace(toStatus) == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	remark, ok := orderWriteStringOf(raw, "remark")
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	blocked := orderBulkShapeBlocked(c, ids)
	if len(blocked) > 0 {
		orderBulkReject(c, blocked)
		return
	}

	reqs := make([]*orderdto.ChangeStatusReq, 0, len(ids))
	for _, rawID := range ids {
		id, _ := strconv.ParseUint(rawID, 10, 64)
		reqs = append(reqs, &orderdto.ChangeStatusReq{
			OrderID:  id,
			ToStatus: toStatus,
			Remark:   remark,

			OperatorType: orderOperatorTypeAdmin,
			OperatorID:   operatorID,
			OperatorName: operatorName,
		})
	}
	if err := h.svc.ChangeStatusBatch(ctx, reqs); err != nil {
		logger.Scene(orderWriteScene).Error(err, "批量流转订单失败")

		orderBulkReject(c, orderBulkReasonBlocked(ids[0], orderReasonText(c, err)))
		return
	}
	response.Success(c, orderWriteBulkResult{Updated: len(ids)})
}

func (h *Handle) orderCancelBulk(c *gin.Context, raw map[string]json.RawMessage) {
	ids, ok := orderWriteBulkShapeOK(c, raw, orderCancelBulkKeys)
	if !ok {
		return
	}
	reason, ok := orderWriteStringOf(raw, "remark")
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	if reason == "" {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.BulkCancelReasonRequired)
		return
	}
	operatorID, operatorName := operatorFromContext(c)
	ctx := c.Request.Context()

	blocked := orderBulkShapeBlocked(c, ids)
	if len(blocked) > 0 {
		orderBulkReject(c, blocked)
		return
	}

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
		logger.Scene(orderWriteScene).Error(err, "批量取消订单失败")
		orderBulkReject(c, orderBulkReasonBlocked(ids[0], orderReasonText(c, err)))
		return
	}
	response.Success(c, orderWriteBulkResult{Updated: len(ids)})
}

func orderBulkShapeBlocked(c *gin.Context, ids []string) []facing.BlockedItem {
	out := make([]facing.BlockedItem, 0)
	for _, rawID := range ids {
		id, perr := strconv.ParseUint(rawID, 10, 64)
		if perr != nil || id == 0 {
			out = append(out, facing.BlockedItem{
				ID: rawID,

				ReasonKey: orderenums.ErrInvalidParam,
				Fallback:  orderenums.FallbackFor(orderenums.ErrInvalidParam),
			})
		}
	}
	return out
}

func orderBulkReasonBlocked(rawID, message string) []facing.BlockedItem {
	return []facing.BlockedItem{{
		ID:       rawID,
		Fallback: message,

		ReasonKey: message,
	}}
}

func orderBulkReject(c *gin.Context, items []facing.BlockedItem) {

	rows := make([]gin.H, 0, len(items))
	for _, it := range items {
		rows = append(rows, gin.H{
			"id":      it.ID,
			"name":    it.Name,
			"message": orderBulkItemText(c, it),
		})
	}
	response.ErrorWithData(c, http.StatusConflict, orderenums.ErrBulkRejected, gin.H{"items": rows})
}

func orderBulkItemText(c *gin.Context, it facing.BlockedItem) string {
	key := strings.TrimSpace(it.ReasonKey)
	if key != "" {
		if text := response.TranslateMessage(c, key); text != key {
			return text
		}
		return key
	}
	return it.Fallback
}
