package inventoryhttp

import (
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

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
	pageview.RenderTemplate(c, InventoryWarehousesPageTemplate, &WarehousesPageResp{
		Menu:                "inventory-warehouses",
		Projects:            toAnySlice(projects),
		SelectedProject:     selected,
		Warehouses:          warehouses,
		WarehouseTypes:      warehouseTypeOptions(),
		WarehouseCreateForm: warehouseFormData(c, selected, false, nil),

		ListQuery: binding.ListQuery(c, inventoryWarehouseBackKeys...),
	})
}

func (h *inventoryPageHandle) InventoryWarehouseCreate(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.WarehouseFormReq](c)
	if err != nil {
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
	form, err := binding.ParseForm[inventorydto.WarehouseFormReq](c)
	if err != nil {
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
	form, err := binding.ParseForm[inventorydto.WarehouseDefaultFormReq](c)
	if err != nil {
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
	form, err := binding.ParseForm[inventorydto.WarehouseDeleteFormReq](c)
	if err != nil {
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
	form, err := binding.ParseForm[inventorydto.WarehouseBulkFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	projectID := form.ProjectID

	ids, berr := bulkids.Normalize(form.IDs)
	if berr != nil {

		inventoryFailMsg(c, inventoryBulkIDsMsg(c, berr))
		return
	}
	if err := h.inventory.BulkDeleteWarehouses(c.Request.Context(), &inventorydto.BulkDeleteWarehouseReq{
		IDs: ids, ProjectID: projectID,
	}); err != nil {
		pageview.FailScene(c, inventoryErrScene, err)
		return
	}
	inventoryWriteOK(c)
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
	for _, w := range rows {
		item := WarehouseRowResp{
			ID: w.ID, Code: w.Code, Name: w.Name, Type: w.Type, Status: w.Status,
			IsDefault:      w.IsDefault,
			Sort:           w.Sort,
			DeductPriority: w.DeductPriority,
			ThirdParty:     w.ThirdParty,
		}
		out = append(out, item)
	}
	return out, nil
}

// warehouseTypeOptions 只交值与顺序；标签由模板按请求语言取词。
func warehouseTypeOptions() []WarehouseTypeOptionResp {
	return []WarehouseTypeOptionResp{
		{Value: inventoryenums.WarehouseTypeSelf},
		{Value: inventoryenums.WarehouseTypeThirdParty},
		{Value: inventoryenums.WarehouseTypeVirtual},
	}
}

func warehouseFormData(c *gin.Context, projectID string, edit bool, row *WarehouseRowResp) *WarehouseFormResp {
	return &WarehouseFormResp{
		IsEdit: edit, Row: row, SelectedProject: projectID,
		WarehouseTypes: warehouseTypeOptions(),
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
