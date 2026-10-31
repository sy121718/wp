package inventoryhttp

import (
	"context"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const inventoryTransfersPath = "/admin/inventory/transfers"

func (h *inventoryTransferPageHandle) InventoryTransfersPage(c *gin.Context) {
	query, ok := binding.Query[inventorydto.ListTransferReq](c)
	if !ok {
		return
	}
	data, err := h.transfersData(c, query)
	if err != nil {
		pageview.RenderTemplate(c, InventoryTransfersPageTemplate, inventoryPageErrorData(c, inventoryMetaTransfers, err))
		return
	}
	pageview.RenderTemplate(c, InventoryTransfersPageTemplate, data)
}

func (h *inventoryTransferPageHandle) transfersData(c *gin.Context,
	query *inventorydto.ListTransferReq) (*TransferListPageResp, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	selected := query.ProjectID
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	list := []*inventorydto.TransferRowResp{}
	var total int64
	if selected != "" {
		query.ProjectID = selected
		if list, err = h.transfers.ListTransfers(ctx, query); err != nil {
			return nil, err
		}
		if total, err = h.transfers.CountTransfers(ctx, query); err != nil {
			return nil, err
		}
	}
	tr := text.TranslateFunc(c)
	page, size := transferPageArgsOf(query.Page, query.Size)
	pg := pagination.Page{Total: total, Page: page, Limit: size}
	pg.Base = reqparams.FilterBaseURL(inventoryTransfersPath, map[string]string{
		"project": selected, "status": query.Status, "keyword": query.Keyword,
	})

	warehouses, werr := h.warehouses(c, selected)
	if werr != nil {
		return nil, werr
	}
	return &TransferListPageResp{
		Warehouses:      warehouses,
		Title:           tr(inventoryenums.InventoryTransfersTitle, "调拨单"),
		Menu:            "inventory-transfers",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Rows:            list,
		FilterStatus:    query.Status,
		Keyword:         query.Keyword,
		StatusOptions:   transferStatusOptions(tr),
		ListQuery:       binding.ListQuery(c, inventoryTransferBackKeys...),
		Pagination:      pg,
	}, nil
}

func (h *inventoryTransferPageHandle) InventoryTransferDetail(c *gin.Context) {
	req, ok := binding.Query[inventorydto.GetTransferReq](c)
	if !ok {
		return
	}
	res, err := h.transfers.GetTransfer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}

func (h *inventoryTransferPageHandle) CreateTransfer(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.CreateTransferReq](c)
	if !ok {
		return
	}
	res, err := h.transfers.CreateTransfer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}

func (h *inventoryTransferPageHandle) ShipTransfer(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.TransferShipReq](c)
	if !ok {
		return
	}
	res, err := h.transfers.ShipTransfer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}

func (h *inventoryTransferPageHandle) ReceiveTransfer(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.TransferReceiveReq](c)
	if !ok {
		return
	}
	res, err := h.transfers.ReceiveTransfer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}

func (h *inventoryTransferPageHandle) InventoryTransferStockOptions(c *gin.Context) {
	req, ok := binding.Query[inventorydto.TransferStockOptionReq](c)
	if !ok {
		return
	}
	list, err := h.transfers.ListTransferStockOptions(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, list)
}

func (h *inventoryTransferPageHandle) ApproveTransfer(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.TransferApproveReq](c)
	if !ok {
		return
	}

	req.OperatorID = int64(builtin.CurrentUserID(c))
	req.OperatorName = builtin.OperatorName(c)
	res, err := h.transfers.ApproveTransfer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}

func (h *inventoryTransferPageHandle) RejectTransfer(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.TransferRejectReq](c)
	if !ok {
		return
	}
	req.OperatorID = int64(builtin.CurrentUserID(c))
	req.OperatorName = builtin.OperatorName(c)
	res, err := h.transfers.RejectTransfer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}

func (h *inventoryTransferPageHandle) CancelTransfer(c *gin.Context) {
	req, ok := binding.JSON[inventorydto.TransferCancelReq](c)
	if !ok {
		return
	}
	res, err := h.transfers.CancelTransfer(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}

type inventoryTransferPageHandle struct {
	transfers inventoryTransferContract
	projects  projectcontract.ProjectService

	warehouses func(c *gin.Context, projectID string) ([]WarehouseRowResp, error)
}

func NewInventoryTransferPageHandle(transfers inventoryTransferContract,
	projects projectcontract.ProjectService,
	warehouseLister func(c *gin.Context, projectID string) ([]WarehouseRowResp, error)) *inventoryTransferPageHandle {
	return &inventoryTransferPageHandle{transfers: transfers, projects: projects, warehouses: warehouseLister}
}

func transferStatusOptions(tr func(key, fallback string) string) []OptionResp {
	return []OptionResp{
		{Value: "", Label: tr(inventoryenums.InventoryFilterAll, "（全部）")},
		{Value: "requested", Label: tr(inventoryenums.InventoryTransfersStatusRequested, "待审批")},
		{Value: "pending", Label: tr(inventoryenums.InventoryTransfersStatusPending, "待出货")},
		{Value: "in_transit", Label: tr(inventoryenums.InventoryTransfersStatusInTransit, "在途")},
		{Value: "completed", Label: tr(inventoryenums.InventoryTransfersStatusCompleted, "已完成")},
		{Value: "rejected", Label: tr(inventoryenums.InventoryTransfersStatusRejected, "已驳回")},
		{Value: "cancelled", Label: tr(inventoryenums.InventoryTransfersStatusCancelled, "已取消")},
	}
}

func transferPageArgsOf(page, size int) (int, int) {
	outPage, outSize := 1, 50
	if page > 0 {
		outPage = page
	}
	if size > 0 {
		outSize = size
	}
	if outSize > 200 {
		outSize = 200
	}
	return outPage, outSize
}

type inventoryTransferContract interface {
	ListTransfers(ctx context.Context, req *inventorydto.ListTransferReq) ([]*inventorydto.TransferRowResp, error)
	CountTransfers(ctx context.Context, req *inventorydto.ListTransferReq) (int64, error)
	GetTransfer(ctx context.Context, req *inventorydto.GetTransferReq) (*inventorydto.TransferDetailResp, error)
	CreateTransfer(ctx context.Context, req *inventorydto.CreateTransferReq) (*inventorydto.TransferResp, error)
	ListTransferStockOptions(ctx context.Context, req *inventorydto.TransferStockOptionReq) ([]*inventorydto.TransferStockOptionResp, error)
	ApproveTransfer(ctx context.Context, req *inventorydto.TransferApproveReq) (*inventorydto.TransferResp, error)
	RejectTransfer(ctx context.Context, req *inventorydto.TransferRejectReq) (*inventorydto.TransferResp, error)
	ShipTransfer(ctx context.Context, req *inventorydto.TransferShipReq) (*inventorydto.TransferResp, error)
	ReceiveTransfer(ctx context.Context, req *inventorydto.TransferReceiveReq) (*inventorydto.TransferResp, error)
	CancelTransfer(ctx context.Context, req *inventorydto.TransferCancelReq) (*inventorydto.TransferResp, error)
}

func (h *inventoryTransferPageHandle) InventoryTransferListJSON(c *gin.Context) {
	req, ok := binding.Query[inventorydto.ListTransferReq](c)
	if !ok {
		return
	}
	list, err := h.transfers.ListTransfers(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, list)
}
