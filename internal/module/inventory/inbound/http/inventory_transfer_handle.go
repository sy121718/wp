// 调拨单的 API 出口与后台页（迁移 627，W3）。
//
// 六条 API（列表 / 详情 / 建单 / 出货 / 签收 / 取消）+ 一个整页。
// 全部是「绑定 → 调用例 → JSON」：业务判定（凭证必填、可售、状态机）都在 service，
// 控制器不重复判一遍 —— 那样两处判据迟早会分叉（页面放行、服务端拒绝）。
package inventoryhttp

import (
	"context"

	"github.com/gin-gonic/gin"

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

// inventoryTransfersPath 调拨页地址（分页基址的唯一字面量处）。
const inventoryTransfersPath = "/admin/inventory/transfers"

// InventoryTransfersPage 调拨单列表页（GET /admin/inventory/transfers）。
func (h *inventoryTransferPageHandle) InventoryTransfersPage(c *gin.Context) {
	query, ok := binding.Query[inventorydto.ListTransferReq](c)
	if !ok {
		return
	}
	data, err := h.transfersData(c, query)
	if err != nil {
		pageview.Render(c, inventoryPageErrorData(c, inventoryMetaTransfers, err))
		return
	}
	pageview.Render(c, data)
}

// transfersData 取齐调拨页的数据并组成视图。
func (h *inventoryTransferPageHandle) transfersData(c *gin.Context,
	query *inventorydto.ListTransferReq) (gin.H, error) {
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
	// 源仓 / 目标仓下拉（建单抽屉用）：与库存页同一份仓库列表。
	warehouses, werr := h.warehouses(c, selected)
	if werr != nil {
		return nil, werr
	}
	return gin.H{
		"Warehouses":      warehouses,
		"title":           tr(inventoryenums.InventoryTransfersTitle, "调拨单"),
		"menu":            "inventory-transfers",
		"Projects":        projects,
		"SelectedProject": selected,
		"Rows":            list,
		"FilterStatus":    query.Status,
		"Keyword":         query.Keyword,
		"StatusOptions":   transferStatusOptions(tr),
		"ListQuery":       binding.ListQuery(c, inventoryTransferBackKeys...),
		"Pagination":      pg,
	}, nil
}

// InventoryTransferDetail 调拨单详情（GET /api/inventory/transfer/get，含三类凭证与候选库存行）。
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

// InventoryTransferCreate 建调拨单（POST /api/inventory/transfer/create）。
func (h *inventoryTransferPageHandle) InventoryTransferCreate(c *gin.Context) {
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

// InventoryTransferShip 出货（POST /api/inventory/transfer/ship）：必须带出货凭证。
func (h *inventoryTransferPageHandle) InventoryTransferShip(c *gin.Context) {
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

// InventoryTransferReceive 签收（POST /api/inventory/transfer/receive）：必须带签收凭证。
func (h *inventoryTransferPageHandle) InventoryTransferReceive(c *gin.Context) {
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

// InventoryTransferCancel 取消（POST /api/inventory/transfer/cancel，仅 pending）。
func (h *inventoryTransferPageHandle) InventoryTransferCancel(c *gin.Context) {
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

// inventoryTransferPageHandle 调拨页的处理器。
type inventoryTransferPageHandle struct {
	transfers inventoryTransferContract
	projects  projectcontract.ProjectService
	// warehouses 仓库下拉（与库存页同一份仓库列表）：注入 listWarehouses 而不是
	// 再持一个 InventoryService —— 那一屏只要「列仓库」一条能力。
	warehouses func(c *gin.Context, projectID string) ([]inventorydto.WarehouseRowResp, error)
}

// NewInventoryTransferPageHandle 构造。
//
// warehouseLister 由调用方传（库存页句柄的方法）：仓库下拉的口径与库存页共用一份，
// 两边各查一次会让「默认仓徽标」这类展示在同一份数据上出现两种说法。
func NewInventoryTransferPageHandle(transfers inventoryTransferContract,
	projects projectcontract.ProjectService,
	warehouseLister func(c *gin.Context, projectID string) ([]inventorydto.WarehouseRowResp, error)) *inventoryTransferPageHandle {
	return &inventoryTransferPageHandle{transfers: transfers, projects: projects, warehouses: warehouseLister}
}

// transferStatusOptions 调拨单状态筛选（四态）。
func transferStatusOptions(tr func(key, fallback string) string) []inventorydto.OptionResp {
	return []inventorydto.OptionResp{
		{Value: "", Label: tr(inventoryenums.InventoryFilterAll, "（全部）")},
		{Value: "pending", Label: tr(inventoryenums.InventoryTransfersStatusPending, "待出货")},
		{Value: "in_transit", Label: tr(inventoryenums.InventoryTransfersStatusInTransit, "在途")},
		{Value: "completed", Label: tr(inventoryenums.InventoryTransfersStatusCompleted, "已完成")},
		{Value: "cancelled", Label: tr(inventoryenums.InventoryTransfersStatusCancelled, "已取消")},
	}
}

// transferPageArgsOf 分页参数归一（与 service 的 transferPageArgs 同口径）。
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

// inventoryTransferContract 调拨页需要的契约能力（比整个 InventoryService 窄）。
//
// 单独声明而不是在 handler 里持 InventoryService：这一屏只要「列表 / 总数 / 详情 /
// 四个写口」，拿全量契约会让读代码的人看不出它的边界。
type inventoryTransferContract interface {
	ListTransfers(ctx context.Context, req *inventorydto.ListTransferReq) ([]*inventorydto.TransferRowResp, error)
	CountTransfers(ctx context.Context, req *inventorydto.ListTransferReq) (int64, error)
	GetTransfer(ctx context.Context, req *inventorydto.GetTransferReq) (*inventorydto.TransferDetailResp, error)
	CreateTransfer(ctx context.Context, req *inventorydto.CreateTransferReq) (*inventorydto.TransferResp, error)
	ShipTransfer(ctx context.Context, req *inventorydto.TransferShipReq) (*inventorydto.TransferResp, error)
	ReceiveTransfer(ctx context.Context, req *inventorydto.TransferReceiveReq) (*inventorydto.TransferResp, error)
	CancelTransfer(ctx context.Context, req *inventorydto.TransferCancelReq) (*inventorydto.TransferResp, error)
}

// InventoryTransferListJSON 调拨单列表（GET /api/inventory/transfer/list，JSON 形状）。
//
// 与页面视图分开：页面出口要「工程下拉 + 分页条 + 取词」，JSON 出口只给数据行 ——
// 两个出口共用同一个 service 调用，形状不同不构成第二份实现。
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
