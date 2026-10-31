package orderhttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

type Handle struct {
	svc ordercontract.OrderService
}

func NewHandle(svc ordercontract.OrderService) *Handle { return &Handle{svc: svc} }

func operatorFromContext(c *gin.Context) (id uint64, name string) {
	name = strings.TrimSpace(builtin.GetUsername(c))
	if v, exists := c.Get("user_id"); exists {
		switch t := v.(type) {
		case uint64:
			id = t
		case int64:
			id = uint64(t)
		case int:
			id = uint64(t)
		case string:
			if n, perr := strconv.ParseUint(t, 10, 64); perr == nil {
				id = n
			}
		}
	}
	return id, name
}

func (h *Handle) CreateOrder(c *gin.Context) {
	req, ok := binding.JSON[orderdto.CreateOrderReq](c)
	if !ok {
		return
	}

	req.IPAddress = c.ClientIP()
	req.UserAgent = c.Request.UserAgent()
	if id, name := operatorFromContext(c); id != 0 || name != "" {
		req.CreateBy = id
	}

	res, err := h.svc.CreateAPIOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCreateSuccess, res)
}

func (h *Handle) GetOrder(c *gin.Context) {
	req, ok := binding.Query[orderdto.GetOrderReq](c)
	if !ok {
		return
	}
	if req.OrderID == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) ListOrders(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListOrderReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ListOrders(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) ChangeStatus(c *gin.Context) {
	raw, ok := orderWriteBody(c)
	if !ok {
		return
	}
	if raw != nil && orderWriteHasKey(raw, "ids") {
		h.orderStatusBulk(c, raw)
		return
	}
	req, ok := binding.JSON[orderdto.ChangeStatusReq](c)
	if !ok {
		return
	}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	if err := h.svc.ChangeStatus(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgStatusChanged, nil)
}

func (h *Handle) CancelOrder(c *gin.Context) {
	raw, ok := orderWriteBody(c)
	if !ok {
		return
	}
	if raw != nil && orderWriteHasKey(raw, "ids") {
		h.orderCancelBulk(c, raw)
		return
	}
	req, ok := binding.JSON[orderdto.CancelOrderReq](c)
	if !ok {
		return
	}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	resp, err := h.svc.CancelOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}

	response.SuccessWithMessage(c, orderenums.MsgCancelled, resp)
}

func (h *Handle) RefundOrder(c *gin.Context) {
	req, ok := binding.JSON[orderdto.RefundOrderReq](c)
	if !ok {
		return
	}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	if err := h.svc.RefundOrder(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgRefunded, nil)
}

func (h *Handle) ListItems(c *gin.Context) {
	req, ok := binding.Query[orderdto.GetOrderReq](c)
	if !ok {
		return
	}
	if req.OrderID == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res.Items)
}

func (h *Handle) ListLogs(c *gin.Context) {
	req, ok := binding.Query[orderdto.GetOrderReq](c)
	if !ok {
		return
	}
	if req.OrderID == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res.Logs)
}

func (h *Handle) ListCoupons(c *gin.Context) {
	req, ok := binding.Query[orderdto.CouponListReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ListCoupons(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) GetCoupon(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("couponId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetCoupon(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) CreateCoupon(c *gin.Context) {
	req, ok := binding.Form[orderdto.CouponSaveReq](c)
	if !ok {
		return
	}

	req.StartsAt = couponTimeInputValue(req.StartsAt)
	req.EndsAt = couponTimeInputValue(req.EndsAt)
	applyCouponOperator(c, req)
	res, err := h.svc.CreateCoupon(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCouponCreated, res)
}

func (h *Handle) UpdateCoupon(c *gin.Context) {
	raw, ok := orderWriteBody(c)
	if !ok {
		return
	}
	if orderWriteHasKey(raw, "ids") {
		if !orderWriteShapeOK(raw, orderCouponToggleBulkKeys) {
			response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
			return
		}
		h.couponToggleBulk(c, raw)
		return
	}
	req, ok := binding.Form[orderdto.CouponSaveReq](c)
	if !ok {
		return
	}

	req.StartsAt = couponTimeInputValue(req.StartsAt)
	req.EndsAt = couponTimeInputValue(req.EndsAt)
	applyCouponOperator(c, req)
	res, err := h.svc.UpdateCoupon(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCouponUpdated, res)
}

func (h *Handle) DeleteCoupon(c *gin.Context) {
	raw, ok := orderWriteBody(c)
	if !ok {
		return
	}
	hasIDs := orderWriteHasKey(raw, "ids")
	if hasIDs {
		if orderWriteHasKey(raw, "id") || orderWriteHasKey(raw, "couponId") {

			response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
			return
		}
		if !orderWriteShapeOK(raw, orderCouponDeleteBulkKeys) {
			response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
			return
		}
		h.couponDeleteBulk(c, raw)
		return
	}
	if !orderWriteShapeOK(raw, orderCouponDeleteSingleKeys) {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	idKey, ok := orderCouponDeleteIDKey(raw)
	if !ok {

		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	id, ok := orderWriteUintOf(raw, idKey)
	if !ok || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteCoupon(c.Request.Context(), id); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCouponDeleted, nil)
}

func (h *Handle) ValidateCoupon(c *gin.Context) {
	req, ok := binding.Query[orderdto.CouponValidateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ValidateCoupon(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) ListCouponRedemptions(c *gin.Context) {
	req, ok := binding.Query[orderdto.CouponRedemptionListReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ListCouponRedemptions(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) AuditCouponCounts(c *gin.Context) {
	req, ok := binding.Query[orderdto.CouponCountAuditReq](c)
	if !ok {
		return
	}
	res, err := h.svc.AuditCouponCounts(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func localizeReturnLabels(c *gin.Context, resps ...*orderdto.ReturnResp) {
	tr := i18n.TranslateFunc(response.RequestLanguage(c))
	for _, r := range resps {
		if r == nil {
			continue
		}
		r.StatusLabel = returnStatusText(tr, r.Status)
	}
}

func (h *Handle) ListReturns(c *gin.Context) {
	req, ok := binding.Query[orderdto.ReturnListReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ListReturns(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	if res != nil {
		localizeReturnLabels(c, res.List...)
	}
	response.Success(c, res)
}

func (h *Handle) GetReturn(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("returnId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, gerr := h.svc.GetReturn(c.Request.Context(), id)
	if gerr != nil {

		if msg := orderFacingText(gerr.Error()); msg != "" {
			response.ErrorWithMessage(c, http.StatusBadRequest, msg)
		} else {
			response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInternal)
		}
		return
	}
	if res != nil {
		localizeReturnLabels(c, res.Return)
	}
	response.Success(c, res)
}

func (h *Handle) ApproveReturn(c *gin.Context) {
	raw, ok := orderWriteBody(c)
	if !ok {
		return
	}
	if raw != nil && orderWriteHasKey(raw, "ids") {
		h.returnBulkApprove(c, raw)
		return
	}
	req, ok := binding.Form[orderdto.ReturnReviewReq](c)
	if !ok {
		return
	}
	applyReturnOperator(c, req)
	res, err := h.svc.ApproveReturn(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	localizeReturnLabels(c, res)
	response.SuccessWithMessage(c, orderenums.MsgReturnApproved, res)
}

func (h *Handle) RejectReturn(c *gin.Context) {
	raw, ok := orderWriteBody(c)
	if !ok {
		return
	}
	if raw != nil && orderWriteHasKey(raw, "ids") {
		h.returnBulkReject(c, raw)
		return
	}
	req, ok := binding.Form[orderdto.ReturnReviewReq](c)
	if !ok {
		return
	}
	applyReturnOperator(c, req)
	res, err := h.svc.RejectReturn(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	localizeReturnLabels(c, res)
	response.SuccessWithMessage(c, orderenums.MsgReturnRejected, res)
}

func (h *Handle) ReceiveReturn(c *gin.Context) {
	req, ok := binding.Form[orderdto.ReturnReceiveReq](c)
	if !ok {
		return
	}
	id, name := operatorFromContext(c)
	req.OperatorType = "admin"
	req.OperatorID = id
	req.OperatorName = name
	res, err := h.svc.ReceiveReturn(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	localizeReturnLabels(c, res)
	response.SuccessWithMessage(c, orderenums.MsgReturnReceived, res)
}

func (h *Handle) UpdateOrderNote(c *gin.Context) {
	req, ok := binding.Form[orderdto.UpdateOrderNoteReq](c)
	if !ok {
		return
	}
	id, name := operatorFromContext(c)
	req.OperatorType = "admin"
	req.OperatorID = id
	req.OperatorName = name
	res, err := h.svc.UpdateOrderNote(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgNoteUpdated, res)
}

func applyReturnOperator(c *gin.Context, req *orderdto.ReturnReviewReq) {
	id, name := operatorFromContext(c)
	req.OperatorType = "admin"
	req.OperatorID = id
	req.OperatorName = name
}

func applyCouponOperator(c *gin.Context, req *orderdto.CouponSaveReq) {
	id, name := operatorFromContext(c)
	req.OperatorID = id
	req.OperatorName = name
}
