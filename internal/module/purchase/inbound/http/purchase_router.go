package purchasehttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	inventoryenums "go_wp/internal/module/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasemodel "go_wp/internal/module/purchase/model"
	purchaseservice "go_wp/internal/module/purchase/service"
	"go_wp/pkg/binding"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/response"
)

type Handle struct {
	svc purchasecontract.PurchaseService
}

func NewHandle(svc purchasecontract.PurchaseService) *Handle { return &Handle{svc: svc} }

func (h *Handle) CreateSource(c *gin.Context) {
	var req purchasedto.CreateSourceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}

	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.CreateSource(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgCreateSuccess, res)
}

func (h *Handle) UpdateSource(c *gin.Context) {
	var req purchasedto.UpdateSourceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	req.OperatorID = operatorFromContext(c)
	res, err := h.svc.UpdateSource(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgUpdateSuccess, res)
}

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

func (h *Handle) DeleteSource(c *gin.Context) {
	var req purchasedto.DeleteSourceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	req.OperatorID = operatorFromContext(c)
	if err := h.svc.DeleteSource(c.Request.Context(), &req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgDeleteSuccess, nil)
}

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

func (h *Handle) CreatePurchaseOrder(c *gin.Context) {
	var req purchasedto.CreatePurchaseOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.CreatePurchaseOrder(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgCreateSuccess, res)
}

func (h *Handle) UpdatePurchaseOrder(c *gin.Context) {
	var req purchasedto.UpdatePurchaseOrderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.UpdatePurchaseOrder(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgUpdateSuccess, res)
}

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

func (h *Handle) RegisterReceipt(c *gin.Context) {
	var req purchasedto.RegisterReceiptReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.RegisterReceipt(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgReceiptSuccess, res)
}

func (h *Handle) RegisterProductionInbound(c *gin.Context) {
	var req purchasedto.ProductionInboundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.RegisterProductionInbound(c.Request.Context(), &req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, purchaseErrScene, err)
		return
	}
	response.SuccessWithMessage(c, purchaseenums.MsgReceiptSuccess, res)
}

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

func SetupPurchaseRoutes(rg *mw.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	project projectcontract.ProjectService, stock purchasecontract.StockPort) purchasecontract.PurchaseService {
	svc := purchaseservice.NewService(purchasemodel.NewModel(db), project)
	if stock != nil {
		svc.SetStock(stock)
	}
	handle := NewHandle(svc)

	if rg != nil {
		g := rg.Group("/inventory")

		g.GET("/source/list", inventoryenums.InventorySourceList, handle.ListSources)
		g.GET("/source/get", inventoryenums.InventorySourceGet, handle.GetSource)
		g.GET("/source/summary", inventoryenums.InventorySourceSummary, handle.SourceSummary)
		g.POST("/source/create", inventoryenums.InventorySourceCreate, handle.CreateSource)
		g.POST("/source/update", inventoryenums.InventorySourceUpdate, handle.UpdateSource)
		g.POST("/source/delete", inventoryenums.InventorySourceDelete, handle.DeleteSource)

		g.GET("/purchase/list", inventoryenums.InventoryPurchaseList, handle.ListPurchaseOrders)
		g.GET("/purchase/get", inventoryenums.InventoryPurchaseGet, handle.GetPurchaseOrder)
		g.GET("/purchase/history", inventoryenums.InventoryPurchaseHistory, handle.ListPurchaseHistory)
		g.POST("/purchase/create", inventoryenums.InventoryPurchaseCreate, handle.CreatePurchaseOrder)
		g.POST("/purchase/update", inventoryenums.InventoryPurchaseUpdate, handle.UpdatePurchaseOrder)
		g.POST("/purchase/receipt", inventoryenums.InventoryPurchaseReceipt, handle.RegisterReceipt)
		g.POST("/purchase/production", inventoryenums.InventoryPurchaseProduction, handle.RegisterProductionInbound)
	}

	SetupPurchasePages(rg.Registry(), pages, svc, stock, project)

	return svc
}
