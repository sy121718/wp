package inventoryhttp

import (
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
	"go_wp/pkg/view/jet"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

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
	tr := jet.TranslateFunc(jet.Lang(c))

	pageview.RenderTemplate(c, InventoryReasonsPageTemplate, &InventoryPageResp{
		Title:           tr(inventoryenums.InventoryReasonsTitle, "变动原因字典"),
		Menu:            "inventory-reasons",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Reasons:         reasons,
		Directions:      directionOptions(tr),

		ReasonCreateForm: reasonFormData(c, selected),
		ListQuery:        binding.ListQuery(c, inventoryReasonBackKeys...),
	})
}

func (h *inventoryPageHandle) InventoryReasonCreate(c *gin.Context) {
	var form inventorydto.ReasonFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
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
	var form inventorydto.ReasonFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
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
	var form inventorydto.ReasonBulkFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
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
	updated, skipped := 0, 0
	for _, id := range ids {
		if _, err := h.inventory.UpdateReason(c.Request.Context(), &inventorydto.UpdateReasonReq{
			ProjectID: projectID, ID: id, Status: &status,
		}); err != nil {
			skipped++
			continue
		}
		updated++
	}

	switch {
	case skipped > 0:
		inventoryFailMsg(c, inventoryBulkReasonPartial.WithArgs(strconv.Itoa(updated), strconv.Itoa(skipped)))
	case updated > 0:
		inventoryWriteOK(c)
	default:
		inventoryWriteOK(c)
	}
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
	tr := jet.TranslateFunc(jet.Lang(c))
	for _, r := range rows {
		out = append(out, ReasonRowResp{
			ID: r.ID, Code: r.Code, Name: tr(r.Name, r.Code),
			Direction: r.Direction, DirectionLabel: directionLabel(tr, r.Direction),
			IsBuiltin: r.IsBuiltin, Status: r.Status, Sort: r.Sort,
			StatusLabel: statusLabel(tr, r.Status),
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

func directionOptions(tr func(key, fallback string) string) []OptionResp {
	return []OptionResp{
		{Value: inventoryenums.DirectionIn, Label: directionLabel(tr, inventoryenums.DirectionIn)},
		{Value: inventoryenums.DirectionOut, Label: directionLabel(tr, inventoryenums.DirectionOut)},
		{Value: inventoryenums.DirectionAdjust, Label: directionLabel(tr, inventoryenums.DirectionAdjust)},
	}
}

func adjustDirectionOptions(c *gin.Context) []OptionResp {
	tr := jet.TranslateFunc(jet.Lang(c))
	return []OptionResp{
		{Value: inventoryenums.DirectionAdjust, Label: tr(inventoryenums.InventoryAdjustDirectionAdjust, "盘点（填目标绝对量）")},
		{Value: inventoryenums.DirectionOut, Label: tr(inventoryenums.InventoryAdjustDirectionOut, "报损（填本次减少量）")},
	}
}

func directionLabel(tr func(key, fallback string) string, direction string) string {
	switch direction {
	case inventoryenums.DirectionIn:
		return tr(inventoryenums.InventoryDirectionIn, "入库")
	case inventoryenums.DirectionOut:
		return tr(inventoryenums.InventoryDirectionOut, "出库")
	case inventoryenums.DirectionAdjust:
		return tr(inventoryenums.InventoryDirectionAdjust, "调整")
	default:
		return direction
	}
}

func reasonFormData(c *gin.Context, projectID string) *ReasonFormResp {
	return &ReasonFormResp{
		SelectedProject: projectID,
		Directions:      toAnySlice(directionOptions(jet.TranslateFunc(jet.Lang(c)))),
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
