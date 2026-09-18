// inventory_handle.go — 仓库与库存记录 HTTP 入口（issue #15）。
//
// 与商品域接口同形：JSON 入参、pkg/response 出参、消息取 enums。
// 本层只做绑定与转发：默认仓兜底、短码归一、删除守卫一律在 service。
package inventoryhttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	"go_wp/pkg/response"
)

// operatorFromContext 从会话取操作人（issue #19 的变更记录操作人）。
//
// 优先取登录名（留痕要能直接读懂「谁改的」，与库存流水的 operator_id 同口径），
// 缺失时退回数值 user_id，两者都没有则空串（留痕字段允许为空）。
// 本函数不参与任何鉴权判断，只做展示用的文本化。
//
// 不走 shell.CurrentUserIDText：这里要的是「先登录名、后 id 文本」的组合语义，
// 且保留非 int64 原始值分支（脚本 / 测试路径可能写入别的形状），shell 入口只认 int64。
func operatorFromContext(c *gin.Context) (id string) {
	if name := strings.TrimSpace(builtin.GetUsername(c)); name != "" {
		return name
	}
	value, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	case uint64:
		return strconv.FormatUint(v, 10)
	}
	return ""
}

// Handle inventory HTTP 处理器。
type Handle struct {
	svc inventorycontract.InventoryService
}

// NewHandle 构造。
func NewHandle(svc inventorycontract.InventoryService) *Handle { return &Handle{svc: svc} }

// CreateSource 新建货源（issue #17 验收 1/2）。
func (h *Handle) CreateSource(c *gin.Context) {
	req := &inventorydto.CreateSourceReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.CreateSource(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgCreateSuccess, res)
}

// UpdateSource 修改货源（验收 1/2）。
func (h *Handle) UpdateSource(c *gin.Context) {
	req := &inventorydto.UpdateSourceReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.UpdateSource(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgUpdateSuccess, res)
}

// GetSource 货源详情。
func (h *Handle) GetSource(c *gin.Context) {
	req := &inventorydto.GetSourceReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.GetSource(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}

// ListSources 货源列表（验收 4：关联方标志是可筛选的报表维度）。
func (h *Handle) ListSources(c *gin.Context) {
	req := &inventorydto.ListSourceReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	list, err := h.svc.ListSources(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, list)
}

// DeleteSource 删除货源。
func (h *Handle) DeleteSource(c *gin.Context) {
	req := &inventorydto.DeleteSourceReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	// issue #19：变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	if err := h.svc.DeleteSource(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDeleteSuccess, nil)
}

// SourceSummary 货源关联方统计（验收 4）。
func (h *Handle) SourceSummary(c *gin.Context) {
	req := &inventorydto.SourceSummaryReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.SourceSummary(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgListSuccess, res)
}

// CreateWarehouse 新建仓库（验收 1）。
func (h *Handle) CreateWarehouse(c *gin.Context) {
	req := &inventorydto.CreateWarehouseReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.UpdateWarehouseReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.GetWarehouseReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.ListWarehouseReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.DeleteWarehouseReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.EnsureStockReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.GetStockReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.ListStockBySKUReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.ListStockReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.ChangeStockReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.DeductStockReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.ListMovementReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.ListReasonReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.CreateReasonReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
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
	req := &inventorydto.UpdateReasonReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, inventoryenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.UpdateReason(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
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
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
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
		response.ErrorAuto(c, http.StatusBadRequest, "inventory", err)
		return
	}
	response.SuccessWithMessage(c, inventoryenums.MsgDetailSuccess, res)
}
