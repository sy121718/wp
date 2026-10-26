// purchase_router.go — purchase 模块的 **API** 装配（货源 / 采购单 / 入库）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）；
// 后台页面的注册在 purchase_page_router.go。
//
// 权限点字符串沿用 `inventory:source_*` / `inventory:purchase_*`（2026-10 采购从
// inventory 拆出来时**刻意没改**）：sys_permission 里已有这些行、casbin 策略按字符串匹配，
// 改名等于让所有已授权角色在升级后突然失去采购权限 —— 行政成本换不来任何收益。
// 代码侧常量（permission.InventorySourceList 等）同理保留原名。
package purchasehttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasemodel "go_wp/internal/module/purchase/model"
	purchaseservice "go_wp/internal/module/purchase/service"
	"go_wp/internal/permission"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

// Handle 采购域接口面（JSON）：货源 6 条 + 采购单 7 条。
type Handle struct {
	svc purchasecontract.PurchaseService
}

// NewHandle 构造。
func NewHandle(svc purchasecontract.PurchaseService) *Handle { return &Handle{svc: svc} }

// —— 货源 ——

// CreateSource 新建货源（内部类型自动成为关联方）。
func (h *Handle) CreateSource(c *gin.Context) {
	req, ok := binding.JSON[purchasedto.CreateSourceReq](c)
	if !ok {
		return
	}
	// 变更记录的操作人从会话取（客户端传入被忽略）。
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.CreateSource(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgCreateSuccess, res)
}

// UpdateSource 修改货源。
func (h *Handle) UpdateSource(c *gin.Context) {
	req, ok := binding.JSON[purchasedto.UpdateSourceReq](c)
	if !ok {
		return
	}
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.UpdateSource(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgUpdateSuccess, res)
}

// GetSource 货源详情。
func (h *Handle) GetSource(c *gin.Context) {
	req, ok := binding.Query[purchasedto.GetSourceReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetSource(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgDetailSuccess, res)
}

// ListSources 货源列表（关联方标志是可筛选的报表维度）。
func (h *Handle) ListSources(c *gin.Context) {
	req, ok := binding.Query[purchasedto.ListSourceReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListSources(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgListSuccess, list)
}

// DeleteSource 删除货源。
func (h *Handle) DeleteSource(c *gin.Context) {
	req, ok := binding.JSON[purchasedto.DeleteSourceReq](c)
	if !ok {
		return
	}
	req.OperatorID = operatorFromContext(c)
	if err := h.svc.DeleteSource(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgDeleteSuccess, nil)
}

// SourceSummary 货源关联方统计。
func (h *Handle) SourceSummary(c *gin.Context) {
	req, ok := binding.Query[purchasedto.SourceSummaryReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SourceSummary(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgListSuccess, res)
}

// —— 采购单与入库 ——

// CreatePurchaseOrder 新建采购单。
func (h *Handle) CreatePurchaseOrder(c *gin.Context) {
	req, ok := binding.JSON[purchasedto.CreatePurchaseOrderReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreatePurchaseOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgCreateSuccess, res)
}

// UpdatePurchaseOrder 修改采购单。
func (h *Handle) UpdatePurchaseOrder(c *gin.Context) {
	req, ok := binding.JSON[purchasedto.UpdatePurchaseOrderReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdatePurchaseOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgUpdateSuccess, res)
}

// GetPurchaseOrder 采购单详情（含全部采购行）。
func (h *Handle) GetPurchaseOrder(c *gin.Context) {
	req, ok := binding.Query[purchasedto.GetPurchaseOrderReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetPurchaseOrder(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgDetailSuccess, res)
}

// ListPurchaseOrders 采购单列表。
func (h *Handle) ListPurchaseOrders(c *gin.Context) {
	req, ok := binding.Query[purchasedto.ListPurchaseOrderReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListPurchaseOrders(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgListSuccess, list)
}

// RegisterReceipt 登记采购收货：库存增加 + 流水 + 成本价更新一次完成。
func (h *Handle) RegisterReceipt(c *gin.Context) {
	req, ok := binding.JSON[purchasedto.RegisterReceiptReq](c)
	if !ok {
		return
	}
	res, err := h.svc.RegisterReceipt(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgReceiptSuccess, res)
}

// RegisterProductionInbound 自家工厂生产入库。
func (h *Handle) RegisterProductionInbound(c *gin.Context) {
	req, ok := binding.JSON[purchasedto.ProductionInboundReq](c)
	if !ok {
		return
	}
	res, err := h.svc.RegisterProductionInbound(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgReceiptSuccess, res)
}

// ListPurchaseHistory 某 SKU 的进货历史。
func (h *Handle) ListPurchaseHistory(c *gin.Context) {
	req, ok := binding.Query[purchasedto.ListPurchaseHistoryReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListPurchaseHistory(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgListSuccess, list)
}

// SetupPurchaseRoutes 装配 purchase 模块的路由，返回模块契约。
//
// rg 是 API 组（前缀 /api，三层链），pages 是后台页面组（前缀 /admin）。
// stock 是库存侧的窄端口（收货动库存、仓解析、仓名映射），由装配层用
// inventory/outbound/purchasestock 的适配器注入 —— 本模块不认识库存任何包。
func SetupPurchaseRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService, stock purchasecontract.StockPort) purchasecontract.PurchaseService {
	svc := purchaseservice.NewService(purchasemodel.NewModel(db), project)
	if stock != nil {
		svc.SetStock(stock)
	}
	handle := NewHandle(svc)

	if rg != nil {
		g := rg.Group("/inventory")
		// 货源：一张表承载全部进货来源 —— 外部供应商 / 集团内关联公司 / 自家工厂
		// 用类型区分，关联方标志独立成列（报表区分），config 承载异构对接扩展信息。
		g.GET("/source/list", permission.InventorySourceList, handle.ListSources)
		g.GET("/source/get", permission.InventorySourceGet, handle.GetSource)
		g.GET("/source/summary", permission.InventorySourceSummary, handle.SourceSummary)
		g.POST("/source/create", permission.InventorySourceCreate, handle.CreateSource)
		g.POST("/source/update", permission.InventorySourceUpdate, handle.UpdateSource)
		g.POST("/source/delete", permission.InventorySourceDelete, handle.DeleteSource)

		// 采购单与入库：采购单（来源 = 货源）→ 收货入库（经 StockPort 由库存落账）。
		// receipt 支持按行分批累加已入库数量，带幂等键防重复入库；production 是自家工厂
		// 生产入库（无采购单，成本价手工填写）；history 是某 SKU 的进货历史。
		g.GET("/purchase/list", permission.InventoryPurchaseList, handle.ListPurchaseOrders)
		g.GET("/purchase/get", permission.InventoryPurchaseGet, handle.GetPurchaseOrder)
		g.GET("/purchase/history", permission.InventoryPurchaseHistory, handle.ListPurchaseHistory)
		g.POST("/purchase/create", permission.InventoryPurchaseCreate, handle.CreatePurchaseOrder)
		g.POST("/purchase/update", permission.InventoryPurchaseUpdate, handle.UpdatePurchaseOrder)
		g.POST("/purchase/receipt", permission.InventoryPurchaseReceipt, handle.RegisterReceipt)
		g.POST("/purchase/production", permission.InventoryPurchaseProduction, handle.RegisterProductionInbound)
	}

	// 后台页面（同一入口调用，装配顺序不变）。
	SetupPurchasePages(pages, svc, stock, project)

	return svc
}
