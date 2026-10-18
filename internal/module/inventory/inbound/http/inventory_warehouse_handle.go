// 仓库后台页与仓库写动作（新建 / 编辑 / 设默认 / 删除 / 批量删除）。
package inventoryhttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"strconv"
	"strings"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	bulkids "go_wp/pkg/bulkids"
)

// InventoryWarehousesPage 仓库管理页（从库存主页拆出）。
//
// 为什么拆页而不是折叠：库存管理原先把「仓库配置 / 变动原因字典 / 手动改库存 / 看流水」
// 四件事压在同一页，靠 <details> 收纳 —— 折叠只是把「平铺」换成「叠起来」，
// 一页装多了就该拆页。仓库是与货源同构的独立实体，配一次长期不动，
// 不该占库存日常操作的版面（评审判据见 docs/02-H-admin-page-shell.md §7）。
func (h *inventoryPageHandle) InventoryWarehousesPage(c *gin.Context) (any, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return inventoryPageErrorData(c, inventoryMetaWarehouses, err), nil
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	warehouses, err := h.listWarehouses(c, selected)
	if err != nil {
		return inventoryPageErrorData(c, inventoryMetaWarehouses, err), nil
	}
	tr := shell.TranslateFor(c)
	return gin.H{
		"title":               tr(inventoryenums.InventoryWarehousesTitle, "仓库管理"),
		"menu":                "inventory-warehouses",
		"Projects":            projects,
		"SelectedProject":     selected,
		"Warehouses":          warehouses,
		"WarehouseTypes":      warehouseTypeOptions(c),
		"WarehouseCreateForm": warehouseFormData(c, selected, false, nil),
		// 写动作表单 action 上带的筛选上下文（见 inventory_jump.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventoryWarehouseBackKeys...),
	}, nil
}

// InventoryWarehouseCreate 新建仓库（工程内第一个仓自动成为默认仓）。
func (h *inventoryPageHandle) InventoryWarehouseCreate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.WarehouseFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.CreateWarehouse(c.Request.Context(), form.CreateReq()); err != nil {
		h.warehouseFormFail(c, err)
		return
	}
	inventoryJump(c, true, inventoryDoneMsg, inventoryWarehouseBack(c), inventoryWarehouseBackMsg)
}

// InventoryWarehouseUpdate 修改仓库（名称 / 短码 / 类型 / 排序 / 状态 / 第三方对接配置）。
func (h *inventoryPageHandle) InventoryWarehouseUpdate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.WarehouseFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), form.UpdateReq()); err != nil {
		h.warehouseFormFail(c, err)
		return
	}
	inventoryJump(c, true, inventoryDoneMsg, inventoryWarehouseBack(c), inventoryWarehouseBackMsg)
}

// InventoryWarehouseDefault 切换默认仓（同工程唯一；「未指定仓库」的兜底）。
func (h *inventoryPageHandle) InventoryWarehouseDefault(c *gin.Context) {
	form, ok := binding.Form[inventorydto.WarehouseDefaultFormReq](c)
	if !ok {
		return
	}
	yes := true
	req := &inventorydto.UpdateWarehouseReq{ID: form.ID, IsDefault: &yes}
	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), req); err != nil {
		inventoryJump(c, false, inventoryErrMsg(c, err), inventoryWarehouseBack(c), inventoryWarehouseBackMsg)
		return
	}
	inventoryJump(c, true, inventoryDoneMsg, inventoryWarehouseBack(c), inventoryWarehouseBackMsg)
}

// InventoryWarehouseDelete 删除仓库（默认仓 / 有非零库存时服务端拒绝）。
func (h *inventoryPageHandle) InventoryWarehouseDelete(c *gin.Context) {
	form, ok := binding.Form[inventorydto.WarehouseDeleteFormReq](c)
	if !ok {
		return
	}
	if err := h.inventory.DeleteWarehouse(c.Request.Context(),
		&inventorydto.DeleteWarehouseReq{ID: form.ID, ProjectID: form.ProjectID}); err != nil {
		inventoryJump(c, false, inventoryErrMsg(c, err), inventoryWarehouseBack(c), inventoryWarehouseBackMsg)
		return
	}
	inventoryJump(c, true, inventoryDoneMsg, inventoryWarehouseBack(c), inventoryWarehouseBackMsg)
}

// InventoryWarehousesBulkDelete 批量删除仓库。
//
// 逐条走同一条删除路径：默认仓、仓内仍有非零库存的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染成提示页，避免静默的部分成功（语义未变，只换传输通道）。
func (h *inventoryPageHandle) InventoryWarehousesBulkDelete(c *gin.Context) {
	form, ok := binding.Form[inventorydto.WarehouseBulkFormReq](c)
	if !ok {
		return
	}
	projectID := form.ProjectID
	back := inventoryWarehouseBack(c)
	backMsg := inventoryWarehouseBackMsg
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := bulkids.Normalize(form.IDs)
	if berr != nil {
		// 超限是受控错误（值域只有 Count/Max）：走受控结论出口（inventoryBulkIDsMsg），
		// 而不是把 err.Error() 拼进提示页（提示页不是可信边界，文案必须已归口）。
		inventoryJump(c, false, inventoryBulkIDsMsg(c, berr), back, backMsg)
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
	// 计数是**事实的一部分**：调用点只给已 strconv 的数字，取词与 `%s` 填充在渲染期做
	// （传 int 会被 TranslateArgs 当字符串填，产出 `%!s(int=2)` 这种看起来「像一句结论」的垃圾）。
	switch {
	case skipped > 0:
		inventoryJump(c, false, inventoryBulkWarehousePartial.WithArgs(strconv.Itoa(deleted), strconv.Itoa(skipped)), back, backMsg)
	case deleted > 0:
		inventoryJump(c, true, inventoryBulkWarehouseDone.WithArgs(strconv.Itoa(deleted)), back, backMsg)
	default:
		// 没有选中任何 id（批量条在无勾选时不会提交）：回列表，不渲染提示页 ——
		// 与改造前一致，且提示页不能显示空串（RenderJump 会把空串落成归口文案）。
		redirectWhere(c, back)
	}
}

// listWarehouses 某工程的仓库（模板直接渲染类型徽标、默认仓徽标与状态）。
func (h *inventoryPageHandle) listWarehouses(c *gin.Context, projectID string) (out []inventorydto.WarehouseRowResp, err error) {
	ctx := c.Request.Context()
	out = []inventorydto.WarehouseRowResp{}
	if projectID == "" {
		return out, nil
	}
	rows, err := h.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	tr := shell.TranslateFor(c)
	for _, w := range rows {
		item := inventorydto.WarehouseRowResp{
			ID: w.ID, Code: w.Code, Name: w.Name, Type: w.Type, Status: w.Status,
			IsDefault: w.IsDefault, Sort: w.Sort,
			StatusLabel:   statusLabel(tr, w.Status),
			TypeLabel:     warehouseTypeLabelText(tr, w.Type),
			HasThirdParty: w.Type == inventoryenums.WarehouseTypeThirdParty,
		}
		if w.ThirdParty != nil {
			item.Provider = w.ThirdParty.Provider
			item.ExternalCode = w.ThirdParty.ExternalCode
			item.Address = w.ThirdParty.Address
			item.Contact = w.ThirdParty.Contact
			item.AllowsShipping = w.ThirdParty.AllowsShipping
			item.HasCredential = w.ThirdParty.HasCredential
			item.CredentialMasked = w.ThirdParty.CredentialMasked
			item.SecretRef = w.ThirdParty.SecretRef
		}
		out = append(out, item)
	}
	return out, nil
}

// —— 取词 / 文案 ——

// warehouseTypeOptions 仓库类型下拉项（文案 key 见迁移 243 的 seed）。
func warehouseTypeOptions(c *gin.Context) []gin.H {
	tr := shell.TranslateFor(c)
	return []gin.H{
		{"Value": inventoryenums.WarehouseTypeSelf, "Label": warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeSelf)},
		{"Value": inventoryenums.WarehouseTypeThirdParty, "Label": warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeThirdParty)},
		{"Value": inventoryenums.WarehouseTypeVirtual, "Label": warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeVirtual)},
	}
}

// warehouseTypeLabelText 仓库类型 → 当前语言文案（列表与下拉共用）。
//
// 本函数是仓库类型 → 文案的**唯一一份**映射：service 侧原先另有一份函数体逐字相同的
// warehouseTypeLabelKey，但它全仓没有任何调用点，且 service 层不应持有页面文案
// （中文兜底只属于展示层）—— 合并时直接删除。新增仓库类型改这一处，并同批 seed 词条（迁移 243）。
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

func warehouseFormData(c *gin.Context, projectID string, edit bool, row gin.H) gin.H {
	return gin.H{
		"IsEdit": edit, "Row": row, "SelectedProject": projectID,
		"WarehouseTypes": warehouseTypeOptions(c),
	}
}

func (h *inventoryPageHandle) warehouseFormFail(c *gin.Context, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryJump(c, false, inventoryErrMsg(c, err), inventoryWarehouseBack(c), inventoryWarehouseBackMsg)
		return
	}
	// htmx 分支：4xx + JSON —— htmx 不 swap，抽屉与输入原样保留，admin.js 弹 toast。
	inventoryFailDrawer(c, err)
}