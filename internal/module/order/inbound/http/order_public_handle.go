package orderhttp

import (
	"github.com/gin-gonic/gin"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	usercontract "go_wp/internal/module/user/contract"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type PublicHandle struct {
	svc ordercontract.VisitorOrderPort
}

func (h *PublicHandle) VisitorList(c *gin.Context) {
	req, err := binding.ParseQuery[orderdto.VisitorOrderListReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.UserID = visitorOrderUserID(c)
	data, err := h.svc.ListVisitorOrders(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "order", err)
		return
	}
	response.Success(c, data)
}
func (h *PublicHandle) VisitorDetail(c *gin.Context) {
	req, err := binding.ParseQuery[orderdto.VisitorOrderDetailReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.UserID = visitorOrderUserID(c)
	data, err := h.svc.GetVisitorOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "order", err)
		return
	}
	response.Success(c, data)
}
func (h *PublicHandle) VisitorReturnable(c *gin.Context) {
	req, err := binding.ParseQuery[orderdto.VisitorOrderDetailReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.UserID = visitorOrderUserID(c)
	data, err := h.svc.ReturnableOfOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "order", err)
		return
	}
	response.Success(c, data)
}
func (h *PublicHandle) VisitorReturns(c *gin.Context) {
	req, err := binding.ParseQuery[orderdto.VisitorReturnListReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.UserID = visitorOrderUserID(c)
	data, err := h.svc.ListVisitorReturns(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "order", err)
		return
	}
	response.Success(c, data)
}
func (h *PublicHandle) VisitorRequestReturn(c *gin.Context) {
	req, err := binding.ParseForm[orderdto.ReturnRequestReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	req.UserID = visitorOrderUserID(c)
	req.IPAddress = c.ClientIP()
	data, err := h.svc.RequestReturn(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "order", err)
		return
	}
	response.Success(c, data)
}
func visitorOrderUserID(c *gin.Context) uint64 {
	value, _ := c.Get(usercontract.VisitorContextKey)
	id, _ := value.(uint64)
	return id
}
