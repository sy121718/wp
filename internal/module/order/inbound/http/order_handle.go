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
