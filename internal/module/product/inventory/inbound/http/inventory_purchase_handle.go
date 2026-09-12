// inventory_purchase_handle.go — 采购单与入库 HTTP 入口（issue #18）。
//
// 与既有库存接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：状态推导、超收拒绝、幂等命中、成本价回写一律在 service。
package inventoryhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	"go_wp/pkg/response"
)

// CreatePurchaseOrder 新建采购单（验收 1）。
func (h *Handle) CreatePurchaseOrder(c *gin.Context) {
	req := &inventorydto.CreatePurchaseOrderReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreatePurchaseOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

// UpdatePurchaseOrder 修改采购单。
func (h *Handle) UpdatePurchaseOrder(c *gin.Context) {
	req := &inventorydto.UpdatePurchaseOrderReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdatePurchaseOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}

// GetPurchaseOrder 采购单详情（含全部采购行）。
func (h *Handle) GetPurchaseOrder(c *gin.Context) {
	req := &inventorydto.GetPurchaseOrderReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetPurchaseOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

// ListPurchaseOrders 采购单列表。
func (h *Handle) ListPurchaseOrders(c *gin.Context) {
	req := &inventorydto.ListPurchaseOrderReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListPurchaseOrders(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// RegisterReceipt 登记采购收货（验收 3/4）：库存增加 + 流水 + 成本价更新一次完成。
func (h *Handle) RegisterReceipt(c *gin.Context) {
	req := &inventorydto.RegisterReceiptReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.RegisterReceipt(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgReceiptSuccess, res)
}

// RegisterProductionInbound 自家工厂生产入库（验收 5）。
func (h *Handle) RegisterProductionInbound(c *gin.Context) {
	req := &inventorydto.ProductionInboundReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.RegisterProductionInbound(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgReceiptSuccess, res)
}

// ListPurchaseHistory 某 SKU 的进货历史（验收 6）。
func (h *Handle) ListPurchaseHistory(c *gin.Context) {
	req := &inventorydto.ListPurchaseHistoryReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListPurchaseHistory(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}
