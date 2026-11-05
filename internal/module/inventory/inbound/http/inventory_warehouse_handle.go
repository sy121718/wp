package inventoryhttp

import (
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	bulkids "go_wp/pkg/bulkids"
)

func (h *inventoryPageHandle) InventoryWarehousesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		inventoryFail(c, err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	warehouses, err := h.listWarehouses(c, selected)
	if err != nil {
		inventoryFail(c, err)
		return
	}
	tr := text.TranslateFunc(c)
	pageview.RenderTemplate(c, InventoryWarehousesPageTemplate, &WarehousesPageResp{
		Title:               tr(inventoryenums.InventoryWarehousesTitle, "仓库管理"),
		Menu:                "inventory-warehouses",
		Projects:            toAnySlice(projects),
		SelectedProject:     selected,
		Warehouses:          warehouses,
		WarehouseTypes:      warehouseTypeOptions(c),
		WarehouseCreateForm: warehouseFormData(c, selected, false, nil),

		ListQuery: binding.ListQuery(c, inventoryWarehouseBackKeys...),
	})
}

func (h *inventoryPageHandle) InventoryWarehouseCreate(c *gin.Context) {
	var form inventorydto.WarehouseFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if _, err := h.inventory.CreateWarehouse(c.Request.Context(), form.CreateReq()); err != nil {
		h.warehouseFormFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryWarehouseUpdate(c *gin.Context) {
	var form inventorydto.WarehouseFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), form.UpdateReq()); err != nil {
		h.warehouseFormFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryWarehouseDefault(c *gin.Context) {
	var form inventorydto.WarehouseDefaultFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	yes := true
	req := &inventorydto.UpdateWarehouseReq{ID: form.ID, IsDefault: &yes}
	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), req); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryWarehouseDelete(c *gin.Context) {
	var form inventorydto.WarehouseDeleteFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if err := h.inventory.DeleteWarehouse(c.Request.Context(),
		&inventorydto.DeleteWarehouseReq{ID: form.ID, ProjectID: form.ProjectID}); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryWarehousesBulkDelete(c *gin.Context) {
	var form inventorydto.WarehouseBulkFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	projectID := form.ProjectID

	ids, berr := bulkids.Normalize(form.IDs)
	if berr != nil {

		inventoryFailMsg(c, inventoryBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.inventory.DeleteWarehouse(c.Request.Context(),
			&inventorydto.DeleteWarehouseReq{ID: id, ProjectID: projectID}); err != nil {
			skipped++
			continue
		}
		deleted++
	}

	switch {
	case skipped > 0:
		inventoryFailMsg(c, inventoryBulkWarehousePartial.WithArgs(strconv.Itoa(deleted), strconv.Itoa(skipped)))
	case deleted > 0:
		inventoryWriteOK(c)
	default:

		inventoryWriteOK(c)
	}
}

func (h *inventoryPageHandle) listWarehouses(c *gin.Context, projectID string) (out []WarehouseRowResp, err error) {
	ctx := c.Request.Context()
	out = []WarehouseRowResp{}
	if projectID == "" {
		return out, nil
	}
	rows, err := h.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	tr := text.TranslateFunc(c)
	for _, w := range rows {
		item := WarehouseRowResp{
			ID: w.ID, Code: w.Code, Name: w.Name, Type: w.Type, Status: w.Status,
			IsDefault:      w.IsDefault,
			Sort:           w.Sort,
			DeductPriority: w.DeductPriority,
			StatusLabel:    statusLabel(tr, w.Status),
			TypeLabel:      warehouseTypeLabelText(tr, w.Type),
			ThirdParty:     w.ThirdParty,
		}
		out = append(out, item)
	}
	return out, nil
}

func warehouseTypeOptions(c *gin.Context) []WarehouseTypeOptionResp {
	tr := text.TranslateFunc(c)
	return []WarehouseTypeOptionResp{
		{Value: inventoryenums.WarehouseTypeSelf, Label: warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeSelf)},
		{Value: inventoryenums.WarehouseTypeThirdParty, Label: warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeThirdParty)},
		{Value: inventoryenums.WarehouseTypeVirtual, Label: warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeVirtual)},
	}
}

func warehouseTypeLabelText(tr func(key, fallback string) string, typ string) string {
	switch typ {
	case inventoryenums.WarehouseTypeThirdParty:
		return tr(inventoryenums.InventoryWarehouseTypeThirdParty, "第三方仓")
	case inventoryenums.WarehouseTypeVirtual:
		return tr(inventoryenums.InventoryWarehouseTypeVirtual, "虚拟仓")
	default:
		return tr(inventoryenums.InventoryWarehouseTypeSelf, "自营仓")
	}
}

func warehouseFormData(c *gin.Context, projectID string, edit bool, row *WarehouseRowResp) *WarehouseFormResp {
	return &WarehouseFormResp{
		IsEdit: edit, Row: row, SelectedProject: projectID,
		WarehouseTypes: warehouseTypeOptions(c),
	}
}

func (h *inventoryPageHandle) warehouseFormFail(c *gin.Context, err error) {

	if !isHXRequest(c) {
		inventoryFail(c, err)
		return
	}

	inventoryFail(c, err)
}

func toAnySlice[T any](in []T) []any {
	out := make([]any, 0, len(in))
	for i := range in {
		out = append(out, in[i])
	}
	return out
}
