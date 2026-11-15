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

// shipOrderForm 绑定发货表单：普通字段与三个平行行数组都走显式表单入口，
// 行数组由 ShipOrderReq.BindForm 按出现顺序配对成 []ShipLineReq；
// 操作人身份只从会话取。
func shipOrderForm(c *gin.Context) (*orderdto.ShipOrderReq, bool) {
	req, err := binding.ParseForm[orderdto.ShipOrderReq](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return nil, false
	}
	id, name := operatorFromContext(c)
	req.OperatorID, req.OperatorName = int64(id), name
	return req, true
}

func (h *Handle) ListShipments(c *gin.Context) {
	req, err := binding.ParseQuery[orderdto.ListShipmentReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseQuery[orderdto.PendingShipReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseQuery[orderdto.ListShipmentReq](c)
	if err != nil {
		response.ParamError(c)
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
	req, err := binding.ParseQuery[orderdto.ListCarrierReq](c)
	if err != nil {
		response.ParamError(c)
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
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[orderdto.CarrierCreateReq](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if _, err := h.svc.CreateCarrier(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordCarrierSaved")
}

func (h *Handle) UpdateCarrier(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[orderdto.CarrierUpdateReq](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	if _, err := h.svc.UpdateCarrier(c.Request.Context(), req); err != nil {
		orderWriteFail(c, err)
		return
	}
	orderWriteOK(c, "ordCarrierSaved")
}

func (h *Handle) GetCurrencyRate(c *gin.Context) {
	req, err := binding.ParseQuery[orderdto.GetRateReq](c)
	if err != nil {
		response.ParamError(c)
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
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[orderdto.SetRateReq](c)
	if err != nil {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SetRate(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "order", err)
		return
	}
	response.Success(c, res)
}
