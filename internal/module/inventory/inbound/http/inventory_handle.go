// inventory_handle.go — 仓库与库存记录 HTTP 入口（issue #15）。
//
// 与商品域接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：默认仓兜底、短码归一、删除守卫一律在 service。
package inventoryhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/pkg/response"
)

// Handle inventory HTTP 处理器。
type Handle struct {
	svc inventorycontract.InventoryService
}

// NewHandle 构造。
func NewHandle(svc inventorycontract.InventoryService) *Handle { return &Handle{svc: svc} }

// CreateWarehouse 新建仓库（验收 1）。
func (h *Handle) CreateWarehouse(c *gin.Context) {
	req := &inventorydto.CreateWarehouseReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateWarehouse(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

// UpdateWarehouse 修改仓库（含切换默认仓）。
func (h *Handle) UpdateWarehouse(c *gin.Context) {
	req := &inventorydto.UpdateWarehouseReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateWarehouse(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}

// GetWarehouse 仓库详情。
func (h *Handle) GetWarehouse(c *gin.Context) {
	req := &inventorydto.GetWarehouseReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetWarehouse(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

// ListWarehouses 仓库列表。
func (h *Handle) ListWarehouses(c *gin.Context) {
	req := &inventorydto.ListWarehouseReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListWarehouses(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// DeleteWarehouse 删除仓库（默认仓 / 有非零库存时拒绝）。
func (h *Handle) DeleteWarehouse(c *gin.Context) {
	req := &inventorydto.DeleteWarehouseReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	if err := h.svc.DeleteWarehouse(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDeleteSuccess, nil)
}

// EnsureStock 确保某 SKU 在某仓有库存记录（初始 0；未指定仓兜底默认仓）。
func (h *Handle) EnsureStock(c *gin.Context) {
	req := &inventorydto.EnsureStockReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.EnsureStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

// GetStock 单条库存记录（按 id 或 变体 × 仓库）。
func (h *Handle) GetStock(c *gin.Context) {
	req := &inventorydto.GetStockReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

// ListStocksBySKU 某 SKU 在各仓的库存（验收 3/4）。
func (h *Handle) ListStocksBySKU(c *gin.Context) {
	req := &inventorydto.ListStockBySKUReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListStocksBySKU(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// ListStocks 库存记录列表（按工程 / 仓 / 商品 / 变体 / SKU 过滤 + 分页）。
func (h *Handle) ListStocks(c *gin.Context) {
	req := &inventorydto.ListStockReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListStocks(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}
