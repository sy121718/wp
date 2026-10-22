// 库存原因页与原因写动作（新建 / 编辑 / 批量启停），含出入库方向词条。
package inventoryhttp

import (
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	bulkids "go_wp/pkg/bulkids"
)

// InventoryReasonsPage 变动原因字典页（从库存主页拆出）。
//
// 理由同仓库管理页：原因字典是配置（内置原因不可改、自定义原因偶尔新增），
// 它是「库存变动的取值范围」的定义处，不是库存日常操作的一部分。
//
// 文案来源：name 列存的是 i18n key，页面上显示的是取词结果；自定义原因的文案
// 在保存时写进 sys_i18n（内容 → 文案词条），这里只负责把它取出来渲染。
func (h *inventoryPageHandle) InventoryReasonsPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		pageview.Render(c, inventoryPageErrorData(c, inventoryMetaReasons, err))
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	reasons, err := h.listReasons(c, selected)
	if err != nil {
		pageview.Render(c, inventoryPageErrorData(c, inventoryMetaReasons, err))
		return
	}
	tr := shell.TranslateFor(c)
	pageview.Render(c, gin.H{
		"title":            tr(inventoryenums.InventoryReasonsTitle, "变动原因字典"),
		"menu":             "inventory-reasons",
		"Projects":         projects,
		"SelectedProject":  selected,
		"Reasons":          reasons,
		"Directions":       directionOptions(tr),
		"ReasonCreateForm": reasonFormData(c, selected),
		// 写动作表单 action 上带的筛选上下文（见 inventory_notice_handle.go）。
		"ListQuery": binding.ListQuery(c, inventoryReasonBackKeys...),
	})
}

// —— 写动作（仓库 / 原因 / 库存调整）——

// InventoryReasonCreate 后台表单新建自定义变动原因（内置原因由迁移 seed，只读）。
func (h *inventoryPageHandle) InventoryReasonCreate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.ReasonFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.CreateReason(c.Request.Context(), form.CreateReq()); err != nil {
		h.reasonFormFail(c, err)
		return
	}
	response.Success(c, nil)
}

// InventoryReasonUpdate 后台表单修改自定义变动原因（改名 / 停用 / 排序）。
//
// 内置原因只读：改名一律由服务端拒绝（它的 key 由系统按 code 派生），仅允许停用 / 启用。
func (h *inventoryPageHandle) InventoryReasonUpdate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.ReasonFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.UpdateReason(c.Request.Context(), form.UpdateReq()); err != nil {
		h.reasonEditFormFail(c, err)
		return
	}
	response.Success(c, nil)
}

// InventoryReasonsBulkStatus 按现有更新权限逐条切换原因状态，不触碰名称和排序。
func (h *inventoryPageHandle) InventoryReasonsBulkStatus(c *gin.Context) {
	form, ok := binding.Form[inventorydto.ReasonBulkFormReq](c)
	if !ok {
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
	// 计数是事实的一部分：调用点只给已 strconv 的数字，取词与 `%s` 填充在渲染期做
	// （传 int 会被 TranslateArgs 当字符串填，产出 `%!s(int=2)` 这种看起来「像一句结论」的垃圾）。
	switch {
	case skipped > 0:
		inventoryFailMsg(c, inventoryBulkReasonPartial.WithArgs(strconv.Itoa(updated), strconv.Itoa(skipped)))
	case updated > 0:
		response.Success(c, nil)
	default:
		response.Success(c, nil)
	}
}

// —— 数据装配（模板只渲染，不做查询）——

// listReasons 某工程可见的变动原因（自定义 + 内置；停用的也列出来，便于识别）。
//
// name 列是 i18n key：这里统一取词成文案后再交给模板 —— 页面上永远不该出现
// inventory.reason.purchase_in 这样的裸 key。取不到词条时回退到 code（短、可辨认，
// 且与同一行的 code 列一致），而不是回退到那个又长又不像话的 key。
func (h *inventoryPageHandle) listReasons(c *gin.Context, projectID string) (out []inventorydto.ReasonRowResp, err error) {
	ctx := c.Request.Context()
	out = []inventorydto.ReasonRowResp{}
	if projectID == "" {
		return out, nil
	}
	rows, err := h.inventory.ListReasons(ctx, &inventorydto.ListReasonReq{
		ProjectID: projectID, IncludeDisabled: true,
	})
	if err != nil {
		return nil, err
	}
	tr := shell.TranslateFor(c)
	for _, r := range rows {
		out = append(out, inventorydto.ReasonRowResp{
			ID: r.ID, Code: r.Code, Name: tr(r.Name, r.Code),
			Direction: r.Direction, DirectionLabel: directionLabel(tr, r.Direction),
			IsBuiltin: r.IsBuiltin, Status: r.Status, Sort: r.Sort,
			StatusLabel: statusLabel(tr, r.Status),
		})
	}
	return out, nil
}

// adjustReasonOptions 库存调整入口可选的原因：只保留「盘点调整 / 报损」方向且启用的条目。
//
// 调整入口不做入库（入库必须挂采购单 / 生产单）：把入库原因列进下拉等于鼓励
// 绕过单据直接入库，那正是这次收口要消灭的路径。
func adjustReasonOptions(reasons []inventorydto.ReasonRowResp) (out []inventorydto.ReasonRowResp) {
	out = []inventorydto.ReasonRowResp{}
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

// directionOptions 库存变动方向的可选项（出 / 入 / 调整三类，仅供**筛选**使用）。
func directionOptions(tr func(key, fallback string) string) []inventorydto.OptionResp {
	return []inventorydto.OptionResp{
		{Value: inventoryenums.DirectionIn, Label: directionLabel(tr, inventoryenums.DirectionIn)},
		{Value: inventoryenums.DirectionOut, Label: directionLabel(tr, inventoryenums.DirectionOut)},
		{Value: inventoryenums.DirectionAdjust, Label: directionLabel(tr, inventoryenums.DirectionAdjust)},
	}
}

// adjustDirectionOptions 库存调整入口可选的方向：盘点（adjust）与报损（out）。
//
// 文案走取词而不是硬编码：这是本批新增的用户可见文案，英文界面不该看到中文。
func adjustDirectionOptions(c *gin.Context) []inventorydto.OptionResp {
	tr := shell.TranslateFor(c)
	return []inventorydto.OptionResp{
		{Value: inventoryenums.DirectionAdjust, Label: tr(inventoryenums.InventoryAdjustDirectionAdjust, "盘点（填目标绝对量）")},
		{Value: inventoryenums.DirectionOut, Label: tr(inventoryenums.InventoryAdjustDirectionOut, "报损（填本次减少量）")},
	}
}

// directionLabel 变动方向 → 当前语言展示文案。
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

func reasonFormData(c *gin.Context, projectID string) gin.H {
	return gin.H{
		"SelectedProject": projectID,
		"Directions":      directionOptions(shell.TranslateFor(c)),
	}
}

func (h *inventoryPageHandle) reasonFormFail(c *gin.Context, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryFail(c, err)
		return
	}
	// htmx 分支：4xx + JSON —— 抽屉不 swap，输入原样保留，admin.js 弹 toast。
	inventoryFail(c, err)
}

func (h *inventoryPageHandle) reasonEditFormFail(c *gin.Context, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryFail(c, err)
		return
	}
	// htmx 分支：4xx + JSON —— 抽屉不 swap，输入原样保留，admin.js 弹 toast。
	inventoryFail(c, err)
}
