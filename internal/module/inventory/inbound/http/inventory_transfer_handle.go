package inventoryhttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/internal/middleware/builtin"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
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
		Menu:            "inventory-transfers",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Rows:            list,
		FilterStatus:    query.Status,
		Keyword:         query.Keyword,
		StatusOptions:   transferStatusOptions(),
		ListQuery:       binding.ListQuery(c, inventoryTransferBackKeys...),
		Pagination:      pg,
	}, nil
}

func (h *inventoryTransferPageHandle) InventoryTransferDetail(c *gin.Context) {
	req, ok := binding.Query[inventorydto.GetTransferReq](c)
	if !ok {
		return
	}
	_, err := h.transfers.GetTransfer(c.Request.Context(), req)
	if err != nil {
		pageview.FailScene(c, inventoryErrScene, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryTransferPageHandle) CreateTransfer(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// 明细行是真正的结构化输入：明确 JSON 请求（builder 收集行），输出仍是 Jet 片段。
	req, ok := binding.JSON[inventorydto.CreateTransferReq](c)
	if !ok {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, inventoryenums.Err(inventoryenums.ErrInvalidParam))
		return
	}
	_, err := h.transfers.CreateTransfer(c.Request.Context(), req)
	if err != nil {
		pageview.FailScene(c, inventoryErrScene, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryTransferPageHandle) ShipTransfer(c *gin.Context) {
	runTransferWrite(c, nil, h.transfers.ShipTransfer, inventoryWriteOK)
}

func (h *inventoryTransferPageHandle) ReceiveTransfer(c *gin.Context) {
	runTransferWrite(c, nil, h.transfers.ReceiveTransfer, inventoryWriteOK)
}

func (h *inventoryTransferPageHandle) InventoryTransferStockOptions(c *gin.Context) {
	req, ok := binding.Query[inventorydto.TransferStockOptionReq](c)
	if !ok {
		return
	}
	list, err := h.transfers.ListTransferStockOptions(c.Request.Context(), req)
	if err != nil {
		pageview.FailScene(c, inventoryErrScene, err)
		return
	}
	response.Success(c, list)
}

func (h *inventoryTransferPageHandle) ApproveTransfer(c *gin.Context) {
	runTransferWrite(c, transferApproveOperator, h.transfers.ApproveTransfer, inventoryWriteOK)
}

func (h *inventoryTransferPageHandle) RejectTransfer(c *gin.Context) {
	runTransferWrite(c, transferRejectOperator, h.transfers.RejectTransfer, inventoryWriteOK)
}

func (h *inventoryTransferPageHandle) CancelTransfer(c *gin.Context) {
	runTransferWrite(c, nil, h.transfers.CancelTransfer, inventoryWriteOK)
}

// transferOperatorOf 取审批人：身份只从会话取，不能由表单提交。
func transferOperatorOf(c *gin.Context) (int64, string) {
	return int64(builtin.CurrentUserID(c)), builtin.OperatorName(c)
}

func transferApproveOperator(c *gin.Context, req *inventorydto.TransferApproveReq) {
	req.OperatorID, req.OperatorName = transferOperatorOf(c)
}

func transferRejectOperator(c *gin.Context, req *inventorydto.TransferRejectReq) {
	req.OperatorID, req.OperatorName = transferOperatorOf(c)
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

// transferStatusOptions 只交值与顺序；标签由模板按请求语言取词。
func transferStatusOptions() []OptionResp {
	return []OptionResp{
		{Value: ""},
		{Value: "requested"},
		{Value: "pending"},
		{Value: "in_transit"},
		{Value: "completed"},
		{Value: "rejected"},
		{Value: "cancelled"},
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
		pageview.FailScene(c, inventoryErrScene, err)
		return
	}
	response.Success(c, list)
}

// runTransferWrite 是调拨单写端点的统一骨架：绑定表单 → 可选注入可信字段 →
// 调用 service → 统一成功出口。失败已由这里写出，控制器不再重复判断。
func runTransferWrite[F, R any](c *gin.Context,
	prepare func(*gin.Context, *F),
	call func(context.Context, *F) (R, error),
	success func(*gin.Context)) {
	c.Header("Cache-Control", "no-store")
	var req F
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}
	if prepare != nil {
		prepare(c, &req)
	}
	if _, err := call(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, inventoryErrScene, err)
		return
	}
	success(c)
}
