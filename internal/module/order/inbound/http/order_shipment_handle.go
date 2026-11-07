package orderhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

func (h *Handle) ShipOrder(c *gin.Context) {
	req, ok := shipOrderForm(c)
	if !ok {
		return
	}
	if _, err := h.svc.ShipOrder(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordShipped")
}

// shipOrderForm 绑定发货表单：普通字段走原生 binder，明细行是
// lines[<下标>][<字段>] 的扁平键，由 ShipOrderReq.BindForm 收成结构；
// 操作人身份只从会话取。
func shipOrderForm(c *gin.Context) (*orderdto.ShipOrderReq, bool) {
	req := bindOrderForm[orderdto.ShipOrderReq](c)
	if req == nil {
		return nil, false
	}
	if err := req.BindForm(c.Request.PostForm); err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return nil, false
	}
	id, name := operatorFromContext(c)
	req.OperatorID, req.OperatorName = int64(id), name
	return req, true
}

func (h *Handle) ListShipments(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListShipmentReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListShipments(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, list)
}

func (h *Handle) ListPendingShip(c *gin.Context) {
	req, ok := binding.Query[orderdto.PendingShipReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ListPendingShip(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) ShipForm(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListShipmentReq](c)
	if !ok {
		return
	}
	form, err := h.svc.BuildShipForm(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, form)
}

func (h *Handle) ListCarriers(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListCarrierReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListCarriers(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, list)
}

func (h *Handle) CreateCarrier(c *gin.Context) {
	req := bindOrderForm[orderdto.CarrierCreateReq](c)
	if req == nil {
		return
	}
	if _, err := h.svc.CreateCarrier(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordCarrierSaved")
}

func (h *Handle) UpdateCarrier(c *gin.Context) {
	req := bindOrderForm[orderdto.CarrierUpdateReq](c)
	if req == nil {
		return
	}
	if _, err := h.svc.UpdateCarrier(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordCarrierSaved")
}

func (h *Handle) GetCurrencyRate(c *gin.Context) {
	req, ok := binding.Query[orderdto.GetRateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetRate(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) SetCurrencyRate(c *gin.Context) {
	req := bindOrderForm[orderdto.SetRateReq](c)
	if req == nil {
		return
	}
	res, err := h.svc.SetRate(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "order", err)
		return
	}
	response.Success(c, res)
}
