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
	"go_wp/pkg/facing"
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
	var req orderdto.CreateOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}

	req.IPAddress = c.ClientIP()
	req.UserAgent = c.Request.UserAgent()
	if id, name := operatorFromContext(c); id != 0 || name != "" {
		req.CreateBy = id
	}

	res, err := h.svc.CreateAPIOrder(c.Request.Context(), &req)
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
	c.Header("Cache-Control", "no-store")
	if orderBulkFormIDs(c) != nil {
		h.orderStatusBulk(c)
		return
	}
	form, err := binding.ParseForm[orderdto.OrderStatusForm](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if form.OrderID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req := &orderdto.ChangeStatusReq{
		OrderID:  form.OrderID,
		ToStatus: form.ToStatus,
		Remark:   form.Remark,
	}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	if err := h.svc.ChangeStatus(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordStChanged")
}

func (h *Handle) CancelOrder(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if orderBulkFormIDs(c) != nil {
		h.orderCancelBulk(c)
		return
	}
	form, err := binding.ParseForm[orderdto.OrderCancelForm](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if form.OrderID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req := &orderdto.CancelOrderReq{OrderID: form.OrderID, Reason: form.Reason}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	if _, err := h.svc.CancelOrder(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordCancelled")
}

func (h *Handle) RefundOrder(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[orderdto.OrderRefundForm](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if form.OrderID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req := &orderdto.RefundOrderReq{
		OrderID:       form.OrderID,
		Reason:        form.Reason,
		TransactionID: form.TransactionID,
	}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	if err := h.svc.RefundOrder(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordRefunded")
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
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[orderdto.CouponSaveReq](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req.StartsAt = couponTimeInputValue(req.StartsAt)
	req.EndsAt = couponTimeInputValue(req.EndsAt)
	applyCouponOperator(c, req)
	if _, err := h.svc.CreateCoupon(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteResultNav(c, "ordCpCreated", couponBackURL(c, req.ProjectID))
}

func (h *Handle) UpdateCoupon(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if orderBulkFormIDs(c) != nil {
		h.couponToggleBulk(c)
		return
	}
	req, err := binding.ParseForm[orderdto.CouponSaveReq](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if req.ID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req.StartsAt = couponTimeInputValue(req.StartsAt)
	req.EndsAt = couponTimeInputValue(req.EndsAt)
	applyCouponOperator(c, req)
	if _, err := h.svc.UpdateCoupon(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteResultNav(c, "ordCpUpdated", couponBackURL(c, req.ProjectID))
}

func (h *Handle) DeleteCoupon(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if orderBulkFormIDs(c) != nil {
		h.couponDeleteBulk(c)
		return
	}
	form, err := binding.ParseForm[orderdto.CouponDeleteForm](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if form.ID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteCoupon(c.Request.Context(), form.ID); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordCpDeleted")
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

// localizeReturnLabels 给退货列表/详情补状态文案。
//
// 这是**数据接口**（response.Success 直接把 ReturnResp 交给调用方），状态文案是
// 该字段的对外内容，因此取词走 pkg/response 的正式口，不在这里自建 i18n 闭包。
func localizeReturnLabels(c *gin.Context, resps ...*orderdto.ReturnResp) {
	for _, r := range resps {
		if r == nil {
			continue
		}
		key, fallback := orderenums.ReturnStatusLabel(r.Status)
		r.StatusLabel = response.MsgText(c, facing.Msg{Key: key, Fallback: fallback})
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
	c.Header("Cache-Control", "no-store")
	if orderBulkFormIDs(c) != nil {
		h.returnBulkApprove(c)
		return
	}
	form, err := binding.ParseForm[orderdto.ReturnReviewForm](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if form.ReturnID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req := &orderdto.ReturnReviewReq{
		ReturnID:      form.ReturnID,
		Remark:        form.Remark,
		AutoReceive:   form.AutoReceive,
		WarehouseID:   form.WarehouseID,
		TransactionID: form.TransactionID,
	}
	applyReturnOperator(c, req)
	if _, err := h.svc.ApproveReturn(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordRetApproved")
}

func (h *Handle) RejectReturn(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if orderBulkFormIDs(c) != nil {
		h.returnBulkReject(c)
		return
	}
	form, err := binding.ParseForm[orderdto.ReturnReviewForm](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if form.ReturnID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req := &orderdto.ReturnReviewReq{ReturnID: form.ReturnID, Remark: form.Remark}
	applyReturnOperator(c, req)
	if _, err := h.svc.RejectReturn(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordRetRejected")
}

func (h *Handle) ReceiveReturn(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[orderdto.ReturnReceiveForm](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if form.ReturnID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req := &orderdto.ReturnReceiveReq{
		ReturnID:      form.ReturnID,
		WarehouseID:   form.WarehouseID,
		Remark:        form.Remark,
		TransactionID: form.TransactionID,
	}
	id, name := operatorFromContext(c)
	req.OperatorType = "admin"
	req.OperatorID = id
	req.OperatorName = name
	if _, err := h.svc.ReceiveReturn(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordRetReceived")
}

func (h *Handle) UpdateOrderNote(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, err := binding.ParseForm[orderdto.OrderNoteForm](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if form.OrderID == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	req := &orderdto.UpdateOrderNoteReq{OrderID: form.OrderID, AdminNote: form.AdminNote}
	id, name := operatorFromContext(c)
	req.OperatorType = "admin"
	req.OperatorID = id
	req.OperatorName = name
	if _, err := h.svc.UpdateOrderNote(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordNoteSaved")
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
