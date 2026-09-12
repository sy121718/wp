package orderhttp

// order_handle.go — 订单 HTTP 处理器（BIZ-1 销售侧）。
//
// handle 只做三件事：绑定参数、调 service、输出响应。业务规则一律不在这里。

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/response"
)

// Handle 订单 HTTP 处理器。
type Handle struct {
	svc ordercontract.OrderService
}

// NewHandle 构造。
func NewHandle(svc ordercontract.OrderService) *Handle { return &Handle{svc: svc} }

// operatorFromContext 取当前后台操作人（id + 用户名）。
//
// 客户端传什么都不看：操作人是审计字段，能被伪造的审计等于没有审计。
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

// CreateOrder 建单。
func (h *Handle) CreateOrder(c *gin.Context) {
	req := &orderdto.CreateOrderReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	// 来源与操作人由服务端写入。
	req.IPAddress = c.ClientIP()
	req.UserAgent = c.Request.UserAgent()
	if id, name := operatorFromContext(c); id != 0 || name != "" {
		req.CreateBy = id
		// 后台代客下单：入口标记与操作人一起落
		if req.CreatedVia == "" {
			req.CreatedVia = "admin"
		}
	}
	res, err := h.svc.CreateOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCreateSuccess, res)
}

// GetOrder 订单详情。
func (h *Handle) GetOrder(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("orderId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetOrder(c.Request.Context(), id)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// ListOrders 订单列表。
func (h *Handle) ListOrders(c *gin.Context) {
	req := &orderdto.ListOrderReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListOrders(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// ChangeStatus 状态流转（发货 / 完成）。
func (h *Handle) ChangeStatus(c *gin.Context) {
	req := &orderdto.ChangeStatusReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	if err := h.svc.ChangeStatus(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgStatusChanged, nil)
}

// CancelOrder 取消订单。
func (h *Handle) CancelOrder(c *gin.Context) {
	req := &orderdto.CancelOrderReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	if err := h.svc.CancelOrder(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCancelled, nil)
}

// RefundOrder 退款。
func (h *Handle) RefundOrder(c *gin.Context) {
	req := &orderdto.RefundOrderReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	req.OperatorID, req.OperatorName = operatorFromContext(c)
	req.OperatorType = "admin"
	if err := h.svc.RefundOrder(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgRefunded, nil)
}

// ListItems 某单的订单项。
func (h *Handle) ListItems(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("orderId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetOrder(c.Request.Context(), id)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res.Items)
}

// ListLogs 某单的状态流转流水。
func (h *Handle) ListLogs(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("orderId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetOrder(c.Request.Context(), id)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res.Logs)
}

// ---------------------------------------------------------------------------
// 优惠码（BIZ-1）：后台管理 + 试算。
//
// 试算是 GET（纯读，不占次数）；核销**没有独立入口** —— 它发生在建单事务里。
// 单独暴露一个「核销」接口，必然会被用出「券核销了但单没下成」这种状态。
// ---------------------------------------------------------------------------

// ListCoupons 优惠码列表。
func (h *Handle) ListCoupons(c *gin.Context) {
	req := &orderdto.CouponListReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListCoupons(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// GetCoupon 优惠码详情。
func (h *Handle) GetCoupon(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("couponId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetCoupon(c.Request.Context(), id)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// CreateCoupon 新建优惠码。
func (h *Handle) CreateCoupon(c *gin.Context) {
	req := &orderdto.CouponSaveReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	applyCouponOperator(c, req)
	res, err := h.svc.CreateCoupon(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCouponCreated, res)
}

// UpdateCoupon 修改优惠码（券码不可改：改码等于换一张券）。
func (h *Handle) UpdateCoupon(c *gin.Context) {
	req := &orderdto.CouponSaveReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	applyCouponOperator(c, req)
	res, err := h.svc.UpdateCoupon(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCouponUpdated, res)
}

// DeleteCoupon 删除优惠码（有核销记录的一律拒绝，请改用停用）。
func (h *Handle) DeleteCoupon(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.PostForm("couponId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteCoupon(c.Request.Context(), id); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgCouponDeleted, nil)
}

// ValidateCoupon 优惠码试算（纯读，不占次数）。
func (h *Handle) ValidateCoupon(c *gin.Context) {
	req := &orderdto.CouponValidateReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ValidateCoupon(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// ListCouponRedemptions 核销记录列表。
func (h *Handle) ListCouponRedemptions(c *gin.Context) {
	req := &orderdto.CouponRedemptionListReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListCouponRedemptions(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// ---------------------------------------------------------------------------
// 退货入库（RMA）：后台侧的审核与收货。
//
// 客户侧的「提交申请 / 撤销 / 查自己的」不走这里 —— 它们是访问面的片段能力
//（访客没有权限点，能做的只有「操作自己的订单」）。
// ---------------------------------------------------------------------------

// ListReturns 退货申请列表 + 各状态计数。
func (h *Handle) ListReturns(c *gin.Context) {
	req := &orderdto.ReturnListReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListReturns(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// GetReturn 退货申请详情（含订单摘要与逐行可退数量）。
func (h *Handle) GetReturn(c *gin.Context) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Query("returnId")), 10, 64)
	if err != nil || id == 0 {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	res, gerr := h.svc.GetReturn(c.Request.Context(), id)
	if gerr != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, gerr.Error())
		return
	}
	response.Success(c, res)
}

// ApproveReturn 同意退货（autoReceive=true 时一步完成入库 + 退款）。
func (h *Handle) ApproveReturn(c *gin.Context) {
	req := &orderdto.ReturnReviewReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	applyReturnOperator(c, req)
	res, err := h.svc.ApproveReturn(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgReturnApproved, res)
}

// RejectReturn 拒绝退货（必须给理由）。
func (h *Handle) RejectReturn(c *gin.Context) {
	req := &orderdto.ReturnReviewReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	applyReturnOperator(c, req)
	res, err := h.svc.RejectReturn(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgReturnRejected, res)
}

// ReceiveReturn 确认收货：**先入库、后退款**。
func (h *Handle) ReceiveReturn(c *gin.Context) {
	req := &orderdto.ReturnReceiveReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return
	}
	id, name := operatorFromContext(c)
	req.OperatorType = "admin"
	req.OperatorID = id
	req.OperatorName = name
	res, err := h.svc.ReceiveReturn(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, orderenums.MsgReturnReceived, res)
}

// applyReturnOperator 把当前后台操作人写进审核请求。
func applyReturnOperator(c *gin.Context, req *orderdto.ReturnReviewReq) {
	id, name := operatorFromContext(c)
	req.OperatorType = "admin"
	req.OperatorID = id
	req.OperatorName = name
}

// applyCouponOperator 把当前后台操作人写进请求。
//
// 客户端传什么都不看：操作人是审计字段，能被伪造的审计等于没有审计。
func applyCouponOperator(c *gin.Context, req *orderdto.CouponSaveReq) {
	id, name := operatorFromContext(c)
	req.OperatorID = id
	req.OperatorName = name
}
