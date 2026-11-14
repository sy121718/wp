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

func (h *inventoryPageHandle) InventoryReasonsPage(c *gin.Context) {
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
	reasons, err := h.listReasons(c, selected)
	if err != nil {
		inventoryFail(c, err)
		return
	}
	pageview.RenderTemplate(c, InventoryReasonsPageTemplate, &InventoryPageResp{
		Menu:            "inventory-reasons",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Reasons:         reasons,
		Directions:      directionOptions(),

		ReasonCreateForm: reasonFormData(c, selected),
		ListQuery:        binding.ListQuery(c, inventoryReasonBackKeys...),
	})
}

func (h *inventoryPageHandle) InventoryReasonCreate(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.ReasonFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if _, err := h.inventory.CreateReason(c.Request.Context(), form.CreateReq()); err != nil {
		h.reasonFormFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryReasonUpdate(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.ReasonFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if _, err := h.inventory.UpdateReason(c.Request.Context(), form.UpdateReq()); err != nil {
		h.reasonEditFormFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryReasonsBulkStatus(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.ReasonBulkFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	projectID := form.ProjectID
	if !form.ValidStatus() {
		inventoryFail(c, inventoryenums.Err(inventoryenums.ErrReasonStatusInvalid))
		return
	}
	status := form.StatusValue()
	ids, err := bulkids.Normalize(form.IDs)
	if err != nil {
		inventoryFailMsg(c, inventoryBulkIDsMsg(c, err))
		return
	}
	if len(ids) == 0 {
		inventoryFailMsg(c, inventoryBulkReasonNoneSelected)
		return
	}
	if err := h.inventory.BulkUpdateReasonStatus(c.Request.Context(), &inventorydto.BulkReasonStatusReq{
		IDs: ids, ProjectID: projectID, Status: status,
	}); err != nil {
		pageview.FailScene(c, inventoryErrScene, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) listReasons(c *gin.Context, projectID string) (out []ReasonRowResp, err error) {
	ctx := c.Request.Context()
	out = []ReasonRowResp{}
	if projectID == "" {
		return out, nil
	}
	rows, err := h.inventory.ListReasons(ctx, &inventorydto.ListReasonReq{
		ProjectID: projectID, IncludeDisabled: true,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		// 原因名是文案：内置与自定义原因的 name 都是 i18n key（迁移 241 收口），
		// 文案真源只有 sys_i18n。这里原样交 key，由模板按请求语言取词。
		out = append(out, ReasonRowResp{
			ID: r.ID, Code: r.Code, Name: r.Name,
			Direction: r.Direction,
			IsBuiltin: r.IsBuiltin, Status: r.Status, Sort: r.Sort,
		})
	}
	return out, nil
}

func adjustReasonOptions(reasons []ReasonRowResp) (out []ReasonRowResp) {
	out = []ReasonRowResp{}
	for _, r := range reasons {
		if r.Status != inventoryenums.StatusActive {
			continue
		}
		if r.Direction != inventoryenums.DirectionAdjust && r.Direction != inventoryenums.DirectionOut {
			continue
		}
		out = append(out, r)
	}
	return out
}

// directionOptions 只交值与顺序；标签由模板按请求语言取词。
func directionOptions() []OptionResp {
	return []OptionResp{
		{Value: inventoryenums.DirectionIn},
		{Value: inventoryenums.DirectionOut},
		{Value: inventoryenums.DirectionAdjust},
	}
}

// adjustDirectionOptions 只交值与顺序；标签由模板按请求语言取词。
func adjustDirectionOptions() []OptionResp {
	return []OptionResp{
		{Value: inventoryenums.DirectionAdjust},
		{Value: inventoryenums.DirectionOut},
	}
}

func reasonFormData(c *gin.Context, projectID string) *ReasonFormResp {
	return &ReasonFormResp{
		SelectedProject: projectID,
		Directions:      toAnySlice(directionOptions()),
	}
}

func (h *inventoryPageHandle) reasonFormFail(c *gin.Context, err error) {

	if !isHXRequest(c) {
		inventoryFail(c, err)
		return
	}

	inventoryFail(c, err)
}

func (h *inventoryPageHandle) reasonEditFormFail(c *gin.Context, err error) {

	if !isHXRequest(c) {
		inventoryFail(c, err)
		return
	}

	inventoryFail(c, err)
}
