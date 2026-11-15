package inventoryhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type Handle struct {
	svc inventorycontract.InventoryService
}

func NewHandle(svc inventorycontract.InventoryService) *Handle { return &Handle{svc: svc} }

func (h *Handle) CreateWarehouse(c *gin.Context) {
	var req inventorydto.CreateWarehouseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.CreateWarehouse(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

func (h *Handle) UpdateWarehouse(c *gin.Context) {
	var req inventorydto.UpdateWarehouseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.UpdateWarehouse(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}

func (h *Handle) GetWarehouse(c *gin.Context) {
	req, err := binding.ParseQuery[inventorydto.GetWarehouseReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.GetWarehouse(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

func (h *Handle) ListWarehouses(c *gin.Context) {
	req, err := binding.ParseQuery[inventorydto.ListWarehouseReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.ListWarehouses(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

func (h *Handle) DeleteWarehouse(c *gin.Context) {
	var req inventorydto.DeleteWarehouseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.DeleteWarehouse(c.Request.Context(), &req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDeleteSuccess, nil)
}

func (h *Handle) EnsureStock(c *gin.Context) {
	var req inventorydto.EnsureStockReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.EnsureStock(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

func (h *Handle) GetStock(c *gin.Context) {
	req, err := binding.ParseQuery[inventorydto.GetStockReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.GetStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

func (h *Handle) ListStocksBySKU(c *gin.Context) {
	req, err := binding.ParseQuery[inventorydto.ListStockBySKUReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.ListStocksBySKU(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

func (h *Handle) ListStocks(c *gin.Context) {
	req, err := binding.ParseQuery[inventorydto.ListStockReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.ListStocks(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

func (h *Handle) ChangeStock(c *gin.Context) {
	var req inventorydto.ChangeStockReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.ChangeStock(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgChangeSuccess, res)
}

func (h *Handle) DeductStock(c *gin.Context) {
	var req inventorydto.DeductStockReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.DeductStock(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDeductSuccess, res)
}

func (h *Handle) ListMovements(c *gin.Context) {
	req, err := binding.ParseQuery[inventorydto.ListMovementReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.ListMovements(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

func (h *Handle) ListReasons(c *gin.Context) {
	req, err := binding.ParseQuery[inventorydto.ListReasonReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.ListReasons(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

func (h *Handle) CreateReason(c *gin.Context) {
	var req inventorydto.CreateReasonReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.CreateReason(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

func (h *Handle) UpdateReason(c *gin.Context) {
	var req inventorydto.UpdateReasonReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.UpdateReason(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}
