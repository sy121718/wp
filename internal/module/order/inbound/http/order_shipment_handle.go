package orderhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

func (h *Handle) ShipOrder(c *gin.Context) {
	req, ok := binding.JSON[orderdto.ShipOrderReq](c)
	if !ok {
		return
	}

	{
		id, name := operatorFromContext(c)
		req.OperatorID, req.OperatorName = int64(id), name
	}
	res, err := h.svc.ShipOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
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
	req, ok := binding.JSON[orderdto.CarrierCreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateCarrier(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
}

func (h *Handle) UpdateCarrier(c *gin.Context) {
	req, ok := binding.JSON[orderdto.CarrierUpdateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateCarrier(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "order", err)
		return
	}
	response.Success(c, res)
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
	req, ok := binding.JSON[orderdto.SetRateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SetRate(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "order", err)
		return
	}
	response.Success(c, res)
}
