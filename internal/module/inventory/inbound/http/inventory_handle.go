package inventoryhttp

// 与商品域接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：默认仓兜底、短码归一、删除守卫一律在 service。

// 与既有库存接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：状态推导、超收拒绝、幂等命中、成本价回写一律在 service。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/pkg/binding"
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
	req, ok := binding.JSON[inventorydto.CreateWarehouseReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateWarehouse(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

// UpdateWarehouse 修改仓库（含切换默认仓）。
func (h *Handle) UpdateWarehouse(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.UpdateWarehouseReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateWarehouse(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}

// GetWarehouse 仓库详情。
func (h *Handle) GetWarehouse(c *gin.Context) {
	req, ok := binding.Query[inventorydto.GetWarehouseReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetWarehouse(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

// ListWarehouses 仓库列表。
func (h *Handle) ListWarehouses(c *gin.Context) {
	req, ok := binding.Query[inventorydto.ListWarehouseReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListWarehouses(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// DeleteWarehouse 删除仓库（默认仓 / 有非零库存时拒绝）。
func (h *Handle) DeleteWarehouse(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.DeleteWarehouseReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteWarehouse(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDeleteSuccess, nil)
}

// EnsureStock 确保某 SKU 在某仓有库存记录（初始 0；未指定仓兜底默认仓）。
func (h *Handle) EnsureStock(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.EnsureStockReq](c)
	if !ok {
		return
	}
	res, err := h.svc.EnsureStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

// GetStock 单条库存记录（按 id 或 变体 × 仓库）。
func (h *Handle) GetStock(c *gin.Context) {
	req, ok := binding.Query[inventorydto.GetStockReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

// ListStocksBySKU 某 SKU 在各仓的库存（验收 3/4）。
func (h *Handle) ListStocksBySKU(c *gin.Context) {
	req, ok := binding.Query[inventorydto.ListStockBySKUReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListStocksBySKU(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// ListStocks 库存记录列表（按工程 / 仓 / 商品 / 变体 / SKU 过滤 + 分页）。
func (h *Handle) ListStocks(c *gin.Context) {
	req, ok := binding.Query[inventorydto.ListStockReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListStocks(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// —— 库存变动与流水（issue #16）——

// ChangeStock 按 SKU 增减库存（验收 1/2/3/4）：方向 + 数量 + 原因 + 来源引用。
func (h *Handle) ChangeStock(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.ChangeStockReq](c)
	if !ok {
		return
	}
	res, err := h.svc.ChangeStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgChangeSuccess, res)
}

// DeductStock 按 SKU 扣减（验收 1/5）：不足即整体拒绝，可展开物料清单。
func (h *Handle) DeductStock(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.DeductStockReq](c)
	if !ok {
		return
	}
	res, err := h.svc.DeductStock(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDeductSuccess, res)
}

// ListMovements 库存流水列表（验收 3）。
func (h *Handle) ListMovements(c *gin.Context) {
	req, ok := binding.Query[inventorydto.ListMovementReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListMovements(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// ListReasons 变动原因字典列表（验收 4）。
func (h *Handle) ListReasons(c *gin.Context) {
	req, ok := binding.Query[inventorydto.ListReasonReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListReasons(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// CreateReason 新建自定义变动原因（验收 4）。
func (h *Handle) CreateReason(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.CreateReasonReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateReason(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

// UpdateReason 修改自定义变动原因（内置原因拒绝）。
func (h *Handle) UpdateReason(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.UpdateReasonReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateReason(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}
