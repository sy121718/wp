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

// —— 库存变动与流水（issue #16）——

// ChangeStock 按 SKU 增减库存（验收 1/2/3/4）：方向 + 数量 + 原因 + 来源引用。
func (h *Handle) ChangeStock(c *gin.Context) {
	req := &inventorydto.ChangeStockReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ChangeStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgChangeSuccess, res)
}

// DeductStock 按 SKU 扣减（验收 1/5）：不足即整体拒绝，可展开物料清单。
func (h *Handle) DeductStock(c *gin.Context) {
	req := &inventorydto.DeductStockReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.DeductStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDeductSuccess, res)
}

// ListMovements 库存流水列表（验收 3）。
func (h *Handle) ListMovements(c *gin.Context) {
	req := &inventorydto.ListMovementReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListMovements(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// ListReasons 变动原因字典列表（验收 4）。
func (h *Handle) ListReasons(c *gin.Context) {
	req := &inventorydto.ListReasonReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListReasons(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// CreateReason 新建自定义变动原因（验收 4）。
func (h *Handle) CreateReason(c *gin.Context) {
	req := &inventorydto.CreateReasonReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.CreateReason(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

// UpdateReason 修改自定义变动原因（内置原因拒绝）。
func (h *Handle) UpdateReason(c *gin.Context) {
	req := &inventorydto.UpdateReasonReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateReason(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}

// SetBOM 全量替换某个父 SKU 的物料清单（验收 5）。
func (h *Handle) SetBOM(c *gin.Context) {
	req := &inventorydto.SetBOMReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SetBOM(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}

// GetBOM 查看某个父 SKU 的物料清单。
func (h *Handle) GetBOM(c *gin.Context) {
	req := &inventorydto.GetBOMReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetBOM(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

// SyncStockCache 显式同步商品侧库存缓存（验收 6）。
func (h *Handle) SyncStockCache(c *gin.Context) {
	req := &inventorydto.SyncStockCacheReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SyncStockCache(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgSyncSuccess, res)
}

// ReconcileStockCache 缓存对账（验收 6/7：真源为唯一依据，可修复）。
func (h *Handle) ReconcileStockCache(c *gin.Context) {
	req := &inventorydto.ReconcileStockCacheReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ReconcileStockCache(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgReconcileSuccess, res)
}
