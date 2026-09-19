// inventory_page_handle.go — 后台库存管理页（issue #15 / #16 / 库存域 2026-09 收口）。
//
// 页面定位：**只读视图 + 一个受权限约束的调整入口**。
//
//	· 库存变动不由本页直接录入 —— 入库 / 出库应与单据对应：采购入库走
//	  /admin/inventory/purchases，销售出库走发货，退货入库走退货单，调拨走调拨单，
//	  盘盈亏走盘点单。本页原先内联的「入库 / 出库 / 调整」表单与「生产入库」按钮因此撤掉；
//	  后端能力一个字没动（那些路径仍在用同一套变动契约写真源与流水）。
//	· 保留的唯一写入口是「库存调整（盘点 / 报损）」：盘点（adjust，目标绝对量）与
//	  报损（out）是**没有单据承载**的两类调整，必须能手工落账；入口受
//	  inventory:stock_change 权限点约束，且必须填原因与备注（每一笔都可追溯）。
//	· 流水列表支持 SKU / 仓库 / 方向 / 原因 / **时间**五个维度筛选。
//
// 与商品后台页同一模式：独立于工作台通用 Handle 的页面处理器，只依赖本模块
// 契约、product 契约与 project 契约；GET 渲染完整页，POST 处理完 302 回列表
// （原生表单 + csrf_token 隐藏域）。
//
// 错误回显（硬规则）：**禁止把 err.Error() 铺到页面上** —— 业务错误的 Error() 就是
// enums 常量（也就是 i18n key），直接铺出去页面上会出现 ErrWarehouseCodeTaken 这种裸 key。
// 统一走 inventoryErrText：按请求语言取词、以 key 作兜底；非业务错误只给通用提示
// （与 internal/module/block/inbound/http/block_page_handle.go 的 blockErrText 同形）。
package inventoryhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// 页面路径常量（回跳地址的唯一定义处，避免各 handler 手写字符串抄错）。
const (
	inventoryPagePath         = "/admin/inventory"
	inventoryWarehousesPath   = "/admin/inventory/warehouses"
	inventoryReasonsPath      = "/admin/inventory/reasons"
	inventoryMovementPageSize = 50
)

// inventoryPageHandle 库存后台页处理器。
type inventoryPageHandle struct {
	inventory inventorycontract.InventoryService
	projects  projectcontract.ProjectService
	// products 只用于「选哪个 SKU 看库存」的下拉（商品 → 变体），不参与任何库存判断。
	products productcontract.ProductService
}

// NewInventoryPageHandle 构造。
//
// products 只用于「商品 → 变体」下拉：装配期传入 nil，装配收尾由 SetProductCatalog 补注
// （商品模块与本模块互为依赖，装配顺序上无法在此时拿到商品契约）。
func NewInventoryPageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService, products productcontract.ProductService) *inventoryPageHandle {
	return &inventoryPageHandle{inventory: inventory, projects: projects, products: products}
}

// setProductCatalog 装配期补注商品契约（见 inventory_router.go 的 SetProductCatalog）。
func (h *inventoryPageHandle) setProductCatalog(products productcontract.ProductService) {
	h.products = products
}

// InventoryPage 库存管理页：工程切换 + 流水筛选 + 某 SKU 各仓库存（只读） + 调整入口。
func (h *inventoryPageHandle) InventoryPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	warehouses, err := h.listWarehouses(c, selected)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}
	options, err := h.variantOptions(ctx, selected)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}

	// 查询目标：variantId 优先（下拉选变体），其次直接给 SKU 编码。
	sku := strings.TrimSpace(c.Query("sku"))
	variantID := strings.TrimSpace(c.Query("variantId"))
	if variantID != "" && sku == "" {
		sku = skuOfVariant(options, variantID)
	}
	// 「该 SKU 的各仓库存」按**仓库**逐行展开，而不是只列已有的库存行：
	// 「没有这一行」本身是一个状态（未入库），靠「表里少一行」表达时，
	// 「所有仓都还没入库」与「这个工程没有仓库」在界面上完全一样。
	//
	// 三态（迁移 261）：∞ 无限（track_quantity = false）/ 数字（跟踪且有数量）/
	// 未入库（这个仓没有这条 SKU 的库存行）。
	stockRows := []gin.H{}
	if sku != "" {
		rows, serr := h.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
			ProjectID: selected, SKUCode: sku,
		})
		if serr != nil {
			shell.PageError(c, "inventory", serr)
			return
		}
		tr := shell.TranslateFor(c)
		byWarehouse := make(map[string]*inventorydto.StockResp, len(rows))
		for _, r := range rows {
			byWarehouse[r.WarehouseID] = r
		}
		for _, w := range warehouses {
			warehouseID, _ := w["ID"].(string)
			r := byWarehouse[warehouseID]
			if r == nil {
				// 未入库：该仓没有这条 SKU 的库存行。没有可编辑的对象，
				// 因此数量列给提示、行内表单不渲染（表单要定位到一行）。
				stockRows = append(stockRows, gin.H{
					"WarehouseID": warehouseID, "Present": false,
					"WarehouseName": w["Name"], "WarehouseCode": w["Code"],
					"VariantID": "", "SKUCode": sku, "ExternalSKU": "",
					"TrackQuantity": false, "Quantity": 0, "UpdatedAt": "",
					"QuantityLabel": tr("admin.inventory.stock.notStocked", "未入库"),
					"CostPrice":     nil, "CostLabel": stockCostLabel(tr, nil),
				})
				continue
			}
			stockRows = append(stockRows, gin.H{
				// WarehouseID 是行内编辑（登记外部编码 / 跟踪开关与数量）的定位键之一：
				// (仓库, 变体) 才唯一确定一行。展示用不上它，但表单的 hidden 字段必须给出。
				"WarehouseID": warehouseID, "Present": true,
				"WarehouseName": r.WarehouseName, "WarehouseCode": r.WarehouseCode,
				"VariantID": r.VariantID, "SKUCode": r.SKUCode,
				// 外部编码（迁移 251）：这条货在该仓叫什么。空串 = 该仓用我们自己的 SKU，
				// 模板渲染成 —（与成本同一个「空是合法状态」的口径）。
				"ExternalSKU": r.ExternalSKU,
				// 跟踪开关与数量（迁移 261）：两个都要进模板 ——
				// 只给数量的话，无限（track=false，数量恒 0）与卖光（track=true，数量 0）
				// 在页面上会是同一个 0。
				"TrackQuantity": r.TrackQuantity,
				"Quantity":      r.Quantity,
				"QuantityLabel": stockQuantityLabel(tr, r.TrackQuantity, r.Quantity),
				"UpdatedAt":     r.UpdatedAt,
				// 成本是 (仓库, SKU) 的当前值（迁移 244）：CostPrice 给需要判空的场景
				// （NULL = 尚未核算），CostLabel 是可直出的文案 —— 尚未核算与 0
				//（合法的显式成本）必须能分开显示，所以不能用数字 0 兜底。
				"CostPrice": r.CostPrice, "CostLabel": stockCostLabel(tr, r.CostPrice),
			})
		}
	}

	reasons, err := h.listReasons(c, selected)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}

	// 流水筛选：SKU / 仓库 / 方向 / 原因 / 时间五个维度。原先挂在采购页的「进货历史」
	// 就是「流水按原因（采购入库 / 生产入库）过滤」，不必再单独做一张表。
	filterWarehouse := strings.TrimSpace(c.Query("warehouseId"))
	filterDirection := strings.TrimSpace(c.Query("direction"))
	filterReason := strings.TrimSpace(c.Query("reasonCode"))
	filterTimeFrom := strings.TrimSpace(c.Query("timeFrom"))
	filterTimeTo := strings.TrimSpace(c.Query("timeTo"))
	movements, err := h.listMovements(c, selected, sku, filterWarehouse, filterDirection,
		filterReason, filterTimeFrom, filterTimeTo)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}

	c.HTML(http.StatusOK, "admin/inventory.html", shell.Prepare(c, gin.H{
		"title":            "库存管理",
		"menu":             "inventory",
		"Projects":         projects,
		"SelectedProject":  selected,
		"Warehouses":       warehouses,
		"VariantOptions":   options,
		"SelectedSKU":      sku,
		"SelectedVariant":  variantID,
		"StockRows":        stockRows,
		"Reasons":          reasons,
		"AdjustReasons":    adjustReasonOptions(reasons),
		"AdjustDirections": adjustDirectionOptions(c),
		"Directions":       directionOptions(),
		"Movements":        movements,
		"FilterWarehouse":  filterWarehouse,
		"FilterDirection":  filterDirection,
		"FilterReason":     filterReason,
		"FilterTimeFrom":   filterTimeFrom,
		"FilterTimeTo":     filterTimeTo,
		// 读侧一律经本页的三个出口（inventoryPageErr / Ok / Done）：查询参数不是可信边界。
		"Err": inventoryPageErr(c),
		// 结论回显（PRG）：本页既有写入口用 ?ok=1，行内编辑外部编码用 ?done=1
		//（其它后台页的写法）—— 两种都认，避免「同一页两种结论参数只有一种会显示」。
		"Ok": inventoryConclusion(c),
	}))
}

// InventoryWarehousesPage 仓库管理页（从库存主页拆出）。
//
// 为什么拆页而不是折叠：库存管理原先把「仓库配置 / 变动原因字典 / 手动改库存 / 看流水」
// 四件事压在同一页，靠 <details> 收纳 —— 折叠只是把「平铺」换成「叠起来」，
// 一页装多了就该拆页。仓库是与货源同构的独立实体，配一次长期不动，
// 不该占库存日常操作的版面（评审判据见 docs/02-H-admin-page-shell.md §7）。
func (h *inventoryPageHandle) InventoryWarehousesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	warehouses, err := h.listWarehouses(c, selected)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}
	c.HTML(http.StatusOK, "admin/inventory_warehouses.html", shell.Prepare(c, gin.H{
		"title":           "仓库管理",
		"menu":            "inventory-warehouses",
		"Projects":        projects,
		"SelectedProject": selected,
		"Warehouses":      warehouses,
		"WarehouseTypes":  warehouseTypeOptions(c),
		"Err":             inventoryPageErr(c),
		"Ok":              inventoryPageOk(c),
		"Done":            inventoryPageDone(c),
	}))
}

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
		shell.PageError(c, "inventory", err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	reasons, err := h.listReasons(c, selected)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}
	c.HTML(http.StatusOK, "admin/inventory_reasons.html", shell.Prepare(c, gin.H{
		"title":           "变动原因字典",
		"menu":            "inventory-reasons",
		"Projects":        projects,
		"SelectedProject": selected,
		"Reasons":         reasons,
		"Directions":      directionOptions(),
		"Err":             inventoryPageErr(c),
		"Ok":              inventoryPageOk(c),
	}))
}

// —— 写动作（仓库 / 原因 / 库存调整）——

// InventoryWarehouseCreate 新建仓库（工程内第一个仓自动成为默认仓）。
func (h *inventoryPageHandle) InventoryWarehouseCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	typ := strings.TrimSpace(c.PostForm("type"))
	req := &inventorydto.CreateWarehouseReq{
		ProjectID:  projectID,
		Code:       strings.TrimSpace(c.PostForm("code")),
		Name:       strings.TrimSpace(c.PostForm("name")),
		Type:       typ,
		IsDefault:  c.PostForm("isDefault") != "",
		Sort:       parseIntOr(c.PostForm("sort"), 0),
		ThirdParty: thirdPartyForm(c, typ),
	}
	if _, err := h.inventory.CreateWarehouse(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, inventoryErrURL(c, inventoryWarehousesPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, inventoryURL(inventoryWarehousesPath, projectID, nil))
}

// InventoryWarehouseUpdate 修改仓库（名称 / 短码 / 类型 / 排序 / 状态 / 第三方对接配置）。
func (h *inventoryPageHandle) InventoryWarehouseUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	code := strings.TrimSpace(c.PostForm("code"))
	name := strings.TrimSpace(c.PostForm("name"))
	status := strings.TrimSpace(c.PostForm("status"))
	typ := strings.TrimSpace(c.PostForm("type"))
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	if status == "" {
		status = inventoryenums.StatusActive
	}
	if typ == "" {
		typ = inventoryenums.WarehouseTypeSelf
	}
	req := &inventorydto.UpdateWarehouseReq{
		ID: c.PostForm("id"), Code: &code, Name: &name, Status: &status,
		Type: &typ, Sort: &sortValue,
		ThirdParty: thirdPartyForm(c, typ),
	}
	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, inventoryErrURL(c, inventoryWarehousesPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, inventoryURL(inventoryWarehousesPath, projectID, nil))
}

// InventoryWarehouseDefault 切换默认仓（同工程唯一；「未指定仓库」的兜底）。
func (h *inventoryPageHandle) InventoryWarehouseDefault(c *gin.Context) {
	projectID := c.PostForm("projectId")
	yes := true
	req := &inventorydto.UpdateWarehouseReq{ID: c.PostForm("id"), IsDefault: &yes}
	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, inventoryErrURL(c, inventoryWarehousesPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, inventoryURL(inventoryWarehousesPath, projectID, nil))
}

// InventoryWarehouseDelete 删除仓库（默认仓 / 有非零库存时服务端拒绝）。
func (h *inventoryPageHandle) InventoryWarehouseDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	if err := h.inventory.DeleteWarehouse(c.Request.Context(),
		&inventorydto.DeleteWarehouseReq{ID: c.PostForm("id"), ProjectID: projectID}); err != nil {
		c.Redirect(http.StatusFound, inventoryErrURL(c, inventoryWarehousesPath, projectID, err))
		return
	}
	c.Redirect(http.StatusFound, inventoryURL(inventoryWarehousesPath, projectID, nil))
}

// InventoryWarehousesBulkDelete 批量删除仓库。
//
// 逐条走同一条删除路径：默认仓、仓内仍有非零库存的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」回带仓库管理页，避免静默的部分成功。
func (h *inventoryPageHandle) InventoryWarehousesBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是 shell 的受控错误（值域只有 Count/Max）：走它的受控文案出口，
		// 而不是把 err.Error() 拼进 URL（读侧白名单也只认这条文案的归一形态）。
		extra := url.Values{}
		extra.Set("err", shell.BulkIDsFacingText(c, berr))
		c.Redirect(http.StatusFound, inventoryURL(inventoryWarehousesPath, projectID, extra))
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
	extra := url.Values{}
	switch {
	case skipped > 0:
		extra.Set("err", fmt.Sprintf(inventoryBulkText(c, inventoryBulkWarehousePartial), deleted, skipped))
	case deleted > 0:
		extra.Set("done", fmt.Sprintf(inventoryBulkText(c, inventoryBulkWarehouseDone), deleted))
	}
	c.Redirect(http.StatusFound, inventoryURL(inventoryWarehousesPath, projectID, extra))
}

// InventoryStockChange 后台「库存调整（盘点 / 报损）」。
//
// 方向只允许两类：adjust（盘点：填的是目标绝对量）与 out（报损：填的是本次减少量）。
// 入库（in）与销售出库不在本页 —— 它们必须挂在采购单 / 发货 / 退货单上，
// 由那些单据驱动同一套变动契约（见文件头）。
//
// 原因与备注**都必填**：调整是「没有单据承载」的写入口，没有原因与备注的调整
// 在事后无法解释，等于把账目变成不可审计的。
func (h *inventoryPageHandle) InventoryStockChange(c *gin.Context) {
	projectID := c.PostForm("projectId")
	direction := strings.TrimSpace(c.PostForm("direction"))
	reasonCode := strings.TrimSpace(c.PostForm("reasonCode"))
	remark := strings.TrimSpace(c.PostForm("remark"))
	sku := strings.TrimSpace(c.PostForm("skuCode"))

	respond := func(err error) {
		extra := url.Values{}
		if sku != "" {
			extra.Set("sku", sku)
		}
		if err != nil {
			extra.Set("err", inventoryErrText(c, err))
		} else {
			extra.Set("ok", "1")
		}
		c.Redirect(http.StatusFound, inventoryURL(inventoryPagePath, projectID, extra))
	}

	if direction != inventoryenums.DirectionAdjust && direction != inventoryenums.DirectionOut {
		respond(errors.New(inventoryenums.ErrStockDirectionInvalid))
		return
	}
	if remark == "" {
		respond(errors.New(inventoryenums.ErrStockRemarkRequired))
		return
	}
	req := &inventorydto.ChangeStockReq{
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		Direction:   direction,
		ReasonCode:  reasonCode,
		SourceType:  strings.TrimSpace(c.PostForm("sourceType")),
		SourceRef:   strings.TrimSpace(c.PostForm("sourceRef")),
		Remark:      remark,
		OperatorID:  builtin.GetUsername(c),
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: strings.TrimSpace(c.PostForm("variantId")),
			SKUCode:   sku,
			ProductID: strings.TrimSpace(c.PostForm("productId")),
			Quantity:  parseIntOr(c.PostForm("quantity"), 0),
		}},
	}
	if _, err := h.inventory.ChangeStock(c.Request.Context(), req); err != nil {
		respond(err)
		return
	}
	respond(nil)
}

// InventoryExternalSKUUpdate 库存页行内编辑：登记 / 修改 / 清空某 (仓库, 变体) 行的外部编码。
//
// 为什么需要它：迁移 251 给 inventory_stocks 加了 external_sku，但库存页原先只有只读列 ——
// 早于 251 建的老商品事后没有地方登记外码。本入口就是那个「事后登记」。
//
// 三条口径全部来自 service（本 handler 只透传，不重复实现）：
//
//	· 空串是**合法值**：清空 = 该仓改回用我们自己的 SKU（撤销映射），
//	  不是「保存失败」，也不动 sku_code / quantity / cost_price；
//	· N:1 弱校验：同一仓同一外码必须属于同一个商品（多口味共用合法，跨商品报错）；
//	· 写入只动 external_sku 一列（model.SetExternalSKUByVariantWarehouse）。
//
// 权限：复用库存页既有写入口的权限点（inventory:stock_change，见 inventory_router.go），
// 不新增权限点、不新增 authorizedAPI 路由。
func (h *inventoryPageHandle) InventoryExternalSKUUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	sku := strings.TrimSpace(c.PostForm("skuCode"))
	req := &inventorydto.BindExternalSKUReq{
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		VariantID:   strings.TrimSpace(c.PostForm("variantId")),
		// 这里不做任何加工：归一（去首尾空白 / 长度上限 / 控制字符）是 service 的唯一口径。
		ExternalSKU: c.PostForm("externalSku"),
	}
	extra := url.Values{}
	if sku != "" {
		// 回列表时保留当前查询的 SKU：否则用户登记完外码会被弹回空白页，还得重新选一次。
		extra.Set("sku", sku)
	}
	if _, err := h.inventory.BindExternalSKU(c.Request.Context(), req); err != nil {
		extra.Set("err", inventoryErrText(c, err))
		c.Redirect(http.StatusFound, inventoryURL(inventoryPagePath, projectID, extra))
		return
	}
	extra.Set("done", "1")
	c.Redirect(http.StatusFound, inventoryURL(inventoryPagePath, projectID, extra))
}

// InventoryStockTrackingUpdate 库存页行内编辑：切换某 (仓库, 变体) 行的跟踪开关并写入数量。
//
// 为什么需要它：迁移 261 给 inventory_stocks 加了 track_quantity，而库存页原先只有
// 一个只读的数量列 —— 运营没有任何地方能把「这条货是无限的」改成「按数量跟踪」，
// 也没有地方把卖光的行改回无限。本入口就是那个开关。
//
// 三条口径全部来自 service（本 handler 只透传，不重复实现）：
//
//	· 数量走变动契约（手工调整）：有变动必有流水，行内编辑不是例外；
//	· 不跟踪（无限）的行不接受非 0 数量（ErrStockUntrackedQuantity）；表单在无限态
//	  把数量框禁用并留空，**绝不预填 0** —— 0 是「卖光」这个具体事实，要写就得自己打；
//	· 跟踪态必须显式给数量：留空不等于 0（否则「忘了填」会被静默记成「没货」）。
//
// 权限：复用库存页既有写入口的权限点（inventory:stock_change，见 inventory_router.go），
// 不新增权限点、不新增 authorizedAPI 路由。
func (h *inventoryPageHandle) InventoryStockTrackingUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	sku := strings.TrimSpace(c.PostForm("skuCode"))
	track := c.PostForm("trackQuantity") != ""
	rawQuantity := strings.TrimSpace(c.PostForm("quantity"))

	extra := url.Values{}
	if sku != "" {
		// 回列表时保留当前查询的 SKU：否则用户改完开关会被弹回空白页，还得重新选一次。
		extra.Set("sku", sku)
	}
	respond := func(err error) {
		if err != nil {
			extra.Set("err", inventoryErrText(c, err))
		} else {
			extra.Set("done", "1")
		}
		c.Redirect(http.StatusFound, inventoryURL(inventoryPagePath, projectID, extra))
	}

	// 跟踪态必须显式给数量：无限态的数量框是 disabled 的（浏览器不提交该字段），
	// 所以「空」在这里有两种来源，只有跟踪态下的空是错误。
	if track && rawQuantity == "" {
		respond(errors.New(inventoryenums.ErrStockQuantityRequired))
		return
	}
	req := &inventorydto.UpdateStockTrackingReq{
		ProjectID:     projectID,
		WarehouseID:   strings.TrimSpace(c.PostForm("warehouseId")),
		VariantID:     strings.TrimSpace(c.PostForm("variantId")),
		TrackQuantity: track,
		Quantity:      parseIntOr(rawQuantity, 0),
	}
	if _, err := h.inventory.UpdateStockTracking(c.Request.Context(), req); err != nil {
		respond(err)
		return
	}
	respond(nil)
}

// InventoryReasonCreate 后台表单新建自定义变动原因（内置原因由迁移 seed，只读）。
func (h *inventoryPageHandle) InventoryReasonCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.CreateReasonReq{
		ProjectID: projectID,
		Code:      strings.TrimSpace(c.PostForm("code")),
		Name:      strings.TrimSpace(c.PostForm("name")),
		Direction: strings.TrimSpace(c.PostForm("direction")),
		Sort:      parseIntOr(c.PostForm("sort"), 0),
	}
	if _, err := h.inventory.CreateReason(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, inventoryErrURL(c, inventoryReasonsPath, projectID, err))
		return
	}
	extra := url.Values{}
	extra.Set("ok", "1")
	c.Redirect(http.StatusFound, inventoryURL(inventoryReasonsPath, projectID, extra))
}

// InventoryReasonUpdate 后台表单修改自定义变动原因（改名 / 停用 / 排序）。
//
// 内置原因只读：改名一律由服务端拒绝（它的 key 由系统按 code 派生），仅允许停用 / 启用。
func (h *inventoryPageHandle) InventoryReasonUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.UpdateReasonReq{ProjectID: projectID, ID: c.PostForm("id")}
	if name := strings.TrimSpace(c.PostForm("name")); name != "" {
		req.Name = &name
	}
	if status := strings.TrimSpace(c.PostForm("status")); status != "" {
		req.Status = &status
	}
	if _, err := h.inventory.UpdateReason(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, inventoryErrURL(c, inventoryReasonsPath, projectID, err))
		return
	}
	extra := url.Values{}
	extra.Set("ok", "1")
	c.Redirect(http.StatusFound, inventoryURL(inventoryReasonsPath, projectID, extra))
}

// —— 数据装配（模板只渲染，不做查询）——

// listWarehouses 某工程的仓库（模板直接渲染类型徽标、默认仓徽标与状态）。
func (h *inventoryPageHandle) listWarehouses(c *gin.Context, projectID string) (out []gin.H, err error) {
	ctx := c.Request.Context()
	out = []gin.H{}
	if projectID == "" {
		return out, nil
	}
	rows, err := h.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	tr := shell.TranslateFor(c)
	for _, w := range rows {
		// 第三方配置的各个键**恒存在**（非第三方仓给零值）：模板里直接取值不会因缺键中断
		//（Jet 的缺失键会让整页从那一行起消失，而 HTTP 仍是 200 —— 见 templates/CLAUDE.md）。
		item := gin.H{
			"ID": w.ID, "Code": w.Code, "Name": w.Name, "Type": w.Type, "Status": w.Status,
			"IsDefault": w.IsDefault, "Sort": w.Sort,
			"StatusLabel":      warehouseStatusLabel(w.Status),
			"TypeLabel":        warehouseTypeLabelText(tr, w.Type),
			"HasThirdParty":    w.Type == inventoryenums.WarehouseTypeThirdParty,
			"Provider":         "",
			"ExternalCode":     "",
			"Address":          "",
			"Contact":          "",
			"AllowsShipping":   false,
			"HasCredential":    false,
			"CredentialMasked": "",
			"SecretRef":        "",
		}
		if w.ThirdParty != nil {
			item["ThirdParty"] = w.ThirdParty
			item["Provider"] = w.ThirdParty.Provider
			item["ExternalCode"] = w.ThirdParty.ExternalCode
			item["Address"] = w.ThirdParty.Address
			item["Contact"] = w.ThirdParty.Contact
			item["AllowsShipping"] = w.ThirdParty.AllowsShipping
			item["HasCredential"] = w.ThirdParty.HasCredential
			item["CredentialMasked"] = w.ThirdParty.CredentialMasked
			item["SecretRef"] = w.ThirdParty.SecretRef
		}
		out = append(out, item)
	}
	return out, nil
}

// listReasons 某工程可见的变动原因（自定义 + 内置；停用的也列出来，便于识别）。
//
// name 列是 i18n key：这里统一取词成文案后再交给模板 —— 页面上永远不该出现
// inventory.reason.purchase_in 这样的裸 key。取不到词条时回退到 code（短、可辨认，
// 且与同一行的 code 列一致），而不是回退到那个又长又不像话的 key。
func (h *inventoryPageHandle) listReasons(c *gin.Context, projectID string) (out []gin.H, err error) {
	ctx := c.Request.Context()
	out = []gin.H{}
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
		out = append(out, gin.H{
			"ID": r.ID, "Code": r.Code, "Name": tr(r.Name, r.Code),
			"Direction": r.Direction, "DirectionLabel": directionLabel(r.Direction),
			"IsBuiltin": r.IsBuiltin, "Status": r.Status, "Sort": r.Sort,
			"StatusLabel": reasonStatusLabel(r.Status),
		})
	}
	return out, nil
}

// adjustReasonOptions 库存调整入口可选的原因：只保留「盘点调整 / 报损」方向且启用的条目。
//
// 调整入口不做入库（入库必须挂采购单 / 生产单）：把入库原因列进下拉等于鼓励
// 绕过单据直接入库，那正是这次收口要消灭的路径。
func adjustReasonOptions(reasons []gin.H) (out []gin.H) {
	out = []gin.H{}
	for _, r := range reasons {
		direction, _ := r["Direction"].(string)
		status, _ := r["Status"].(string)
		if status != inventoryenums.StatusActive {
			continue
		}
		if direction != inventoryenums.DirectionAdjust && direction != inventoryenums.DirectionOut {
			continue
		}
		out = append(out, r)
	}
	return out
}

// listMovements 库存流水列表（按 SKU / 仓库 / 方向 / 原因 / 时间过滤）。
func (h *inventoryPageHandle) listMovements(c *gin.Context, projectID, sku, warehouseID,
	direction, reasonCode, timeFrom, timeTo string) (out []gin.H, err error) {
	ctx := c.Request.Context()
	out = []gin.H{}
	if projectID == "" {
		return out, nil
	}
	rows, err := h.inventory.ListMovements(ctx, &inventorydto.ListMovementReq{
		ProjectID: projectID, SKUCode: sku, WarehouseID: warehouseID,
		Direction: direction, ReasonCode: reasonCode,
		TimeFrom: timeFrom, TimeTo: timeTo, Size: inventoryMovementPageSize,
	})
	if err != nil {
		return nil, err
	}
	tr := shell.TranslateFor(c)
	for _, m := range rows {
		out = append(out, gin.H{
			"ID": m.ID, "SKUCode": m.SKUCode, "WarehouseName": m.WarehouseName,
			"WarehouseCode": m.WarehouseCode, "Direction": m.Direction,
			"DirectionLabel": directionLabel(m.Direction),
			"Quantity":       m.Quantity, "QuantityBefore": m.QuantityBefore, "QuantityAfter": m.QuantityAfter,
			"ReasonName": tr(m.ReasonName, m.ReasonCode), "ReasonCode": m.ReasonCode,
			"SourceType": m.SourceType, "SourceRef": m.SourceRef, "Remark": m.Remark,
			"OperatorID": m.OperatorID, "BatchID": m.BatchID, "CreatedAt": m.CreatedAt,
		})
	}
	return out, nil
}

// variantOptions 某工程全部商品的变体下拉项（SKU 查询的入口；上限 100 个商品，与商品页一致）。
func (h *inventoryPageHandle) variantOptions(ctx context.Context, projectID string) (out []gin.H, err error) {
	out = []gin.H{}
	// 商品契约未注入（装配漏接）时下拉为空，页面照常渲染 —— 不因一处装配缺失 500。
	if projectID == "" || h.products == nil {
		return out, nil
	}
	list, err := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Size: 100})
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
		if derr != nil {
			continue
		}
		for _, v := range detail.Variants {
			out = append(out, gin.H{
				"VariantID": v.ID, "ProductID": p.ID, "SKUCode": v.SKUCode,
				"Label": detail.Name + " · " + v.SKUCode,
			})
		}
	}
	return out, nil
}

// skuOfVariant 从下拉项里按变体 id 取 SKU 编码（找不到返回空串，页面提示无匹配）。
func skuOfVariant(options []gin.H, variantID string) string {
	for _, o := range options {
		if id, _ := o["VariantID"].(string); id == variantID {
			code, _ := o["SKUCode"].(string)
			return code
		}
	}
	return ""
}

// thirdPartyForm 读取第三方仓对接配置表单。
//
// 只在类型是第三方仓时才认这些字段（其余类型传 nil）：非第三方仓带配置会被
// service 判成 ErrWarehouseConfigInvalid —— 表单是它们的唯一来源，就在这里先分清。
//
// apiCredential 收到的是**明文**，且只在这一次请求里存在：service 立刻加密后落库，
// 客户端拿到的回显永远是掩码。留空 / 传回掩码都表示「不改凭据」。
func thirdPartyForm(c *gin.Context, typ string) *inventorydto.WarehouseThirdPartyReq {
	if typ != inventoryenums.WarehouseTypeThirdParty {
		return nil
	}
	return &inventorydto.WarehouseThirdPartyReq{
		Provider:        strings.TrimSpace(c.PostForm("provider")),
		ExternalCode:    strings.TrimSpace(c.PostForm("externalCode")),
		Address:         strings.TrimSpace(c.PostForm("address")),
		Contact:         strings.TrimSpace(c.PostForm("contact")),
		AllowsShipping:  c.PostForm("allowsShipping") != "",
		APICredential:   strings.TrimSpace(c.PostForm("apiCredential")),
		ClearCredential: c.PostForm("clearCredential") != "",
		SecretRef:       strings.TrimSpace(c.PostForm("secretRef")),
	}
}

// —— 取词 / 文案 ——

// directionOptions 库存变动方向的可选项（出 / 入 / 调整三类，仅供**筛选**使用）。
func directionOptions() []gin.H {
	return []gin.H{
		{"Value": inventoryenums.DirectionIn, "Label": directionLabel(inventoryenums.DirectionIn)},
		{"Value": inventoryenums.DirectionOut, "Label": directionLabel(inventoryenums.DirectionOut)},
		{"Value": inventoryenums.DirectionAdjust, "Label": directionLabel(inventoryenums.DirectionAdjust)},
	}
}

// adjustDirectionOptions 库存调整入口可选的方向：盘点（adjust）与报损（out）。
//
// 文案走取词而不是硬编码：这是本批新增的用户可见文案，英文界面不该看到中文。
func adjustDirectionOptions(c *gin.Context) []gin.H {
	tr := shell.TranslateFor(c)
	return []gin.H{
		{"Value": inventoryenums.DirectionAdjust, "Label": tr("admin.inventory.adjust.direction.adjust", "盘点（填目标绝对量）")},
		{"Value": inventoryenums.DirectionOut, "Label": tr("admin.inventory.adjust.direction.out", "报损（填本次减少量）")},
	}
}

// warehouseTypeOptions 仓库类型下拉项（文案 key 见迁移 243 的 seed）。
func warehouseTypeOptions(c *gin.Context) []gin.H {
	tr := shell.TranslateFor(c)
	return []gin.H{
		{"Value": inventoryenums.WarehouseTypeSelf, "Label": warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeSelf)},
		{"Value": inventoryenums.WarehouseTypeThirdParty, "Label": warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeThirdParty)},
		{"Value": inventoryenums.WarehouseTypeVirtual, "Label": warehouseTypeLabelText(tr, inventoryenums.WarehouseTypeVirtual)},
	}
}

// warehouseTypeLabelText 仓库类型 → 当前语言文案（下拉用）。
func warehouseTypeLabelText(tr func(key, fallback string) string, typ string) string {
	switch typ {
	case inventoryenums.WarehouseTypeThirdParty:
		return tr("admin.inventory.warehouse.type.third_party", "第三方仓")
	case inventoryenums.WarehouseTypeVirtual:
		return tr("admin.inventory.warehouse.type.virtual", "虚拟仓")
	default:
		return tr("admin.inventory.warehouse.type.self", "自营仓")
	}
}

// directionLabel 变动方向 → 展示文案。
func directionLabel(direction string) string {
	switch direction {
	case inventoryenums.DirectionIn:
		return "入库"
	case inventoryenums.DirectionOut:
		return "出库"
	case inventoryenums.DirectionAdjust:
		return "调整"
	default:
		return direction
	}
}

// reasonStatusLabel 原因状态 → 展示文案。
func reasonStatusLabel(status string) string {
	if status == inventoryenums.StatusDisabled {
		return "已停用"
	}
	return "启用中"
}

// stockCostLabel 库存行的成本展示文案（(仓库, SKU) 的当前成本价，迁移 244）。
//
// 空值不能显示成 0：成本列可空表示**尚未核算**（还没核算过 / 由外部核算后导入），
// 而 0 是合法的显式成本（赠品 / 内部划拨）—— 两者混在一起，运营就再也分不清
// 「这个仓这条 SKU 没成本」和「这条 SKU 不要钱」。
func stockCostLabel(tr func(key, fallback string) string, cost *float64) string {
	if cost == nil {
		return tr("admin.inventory.cost.unknown", "未核算")
	}
	return fmt.Sprintf("%.2f", *cost)
}

// stockQuantityLabel 库存行的数量展示：三态里的前两态（∞ 无限 / 数字）。
//
// 为什么不能直接显示 0：不跟踪的行数量恒为 0（迁移 261 的 CHECK 保证），
// 显示 0 会让「无限」看起来像「没货」—— 无限必须显示成一个与数字明确不同的东西。
// 第三态「未入库」（这个仓没有这一行）不走本函数，由调用点直接取 notStocked 文案。
func stockQuantityLabel(tr func(key, fallback string) string, track bool, quantity int) string {
	if !track {
		return "∞ " + tr("admin.inventory.stock.unlimited", "无限")
	}
	return fmt.Sprintf("%d", quantity)
}

// warehouseStatusLabel 仓库状态 → 展示文案。
func warehouseStatusLabel(status string) string {
	if status == inventoryenums.StatusDisabled {
		return "已停用"
	}
	return "启用中"
}

// —— 回跳地址与错误文案 ——

// inventoryURL 组装后台页回跳地址（PRG）：始终保留工程上下文，extra 为结论文案等附加值。
func inventoryURL(path, projectID string, extra url.Values) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	for key, values := range extra {
		for _, v := range values {
			q.Add(key, v)
		}
	}
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

// inventoryConclusion 读 PRG 结论参数：ok 与 done 等价（见 InventoryPage 的结论回显）。
//
// 两个名字都存在是历史：本页早期写入口用 ok，其它后台页用 done。行内编辑外码沿用后者，
// 但同一个页面上「只有一半的结论会显示」是个纯粹的坑，所以两种都认。
func inventoryConclusion(c *gin.Context) string {
	// 两个名字都过读侧出口（token 收敛 + 白名单）：此前是原样返回查询参数，
	// 于是 ?ok=任意文案 也会被当作结论渲染出来。
	if ok := inventoryPageOk(c); ok != "" {
		return ok
	}
	return inventoryPageDone(c)
}

// inventoryErrURL 失败回跳：把业务错误翻成当前语言的文案再带回列表页。
func inventoryErrURL(c *gin.Context, path, projectID string, err error) string {
	extra := url.Values{}
	if err != nil {
		extra.Set("err", inventoryErrText(c, err))
	}
	return inventoryURL(path, projectID, extra)
}

// inventoryErrInternalFallback 非业务错误（基础设施故障）的兜底文案：中文原文，兼作取词兜底。
const inventoryErrInternalFallback = "系统内部错误，请稍后重试"

// inventoryErrScene 结构化日志的场景名（与模块其它 logger.Scene("inventory") 调用点一致）。
const inventoryErrScene = "inventory"

// inventoryErrText 业务错误 → 当前语言文案。
//
// 业务错误的 Error() 就是 enums 常量，而 enums 常量即 i18n key，所以这里按请求语言取词。
// sys_i18n 里已 seed 的是仓库 / 库存 / 原因 / 外码 / 入库 SKU 这几类（迁移 242/243/252/270）；
// 采购 / 收货 / 生产入库那一组仍未 seed（它们的注册文件不在本批授权内），
// 由下面的兜底表给出中文 —— 未命中兜底表时返回的会是裸 key，所以新错误必须两处都登记。
// 取不到词条时退回 inventoryErrFallbacks 的中文，**不是**那个裸 key ——
// 把裸 key 铺到页面上（ErrExternalSKUInvalid）既不中文也不是一句人能读的话。
// 非业务错误对外只给通用提示，**原文进结构化日志**（场景 + user_id）—— 「不许直出内部错误」
// 之后仍要留下可诊断性：谁在哪个页面撞上了哪条 SQL / 约束，只有日志能回答。
func inventoryErrText(c *gin.Context, err error) string {
	tr := shell.TranslateFor(c)
	key, tail := inventoryErrKey(err)
	if key == "" {
		if err != nil {
			logger.Scene(inventoryErrScene).
				With("user_id", shell.CurrentUserID(c)).
				Error(err, "库存页操作失败（非业务错误，只对外给归口文案）")
		}
		return tr(shell.MsgInternalError, inventoryErrInternalFallback)
	}
	text := tr(key, inventoryErrFallback(key))
	if tail != "" {
		// 补充说明原样跟在译文后：service 写进去的是「为什么被拒」的上下文
		//（例如被哪个商品占了外码），吞掉它就等于让人去猜。
		text += "：" + tail
	}
	return text
}

// —— 读侧回执的收口（?err= / ?ok= / ?done=）——
//
// 写侧已经是受控的（inventoryErrText / inventoryErrURL），但三个页面的读侧此前把 query
// 参数**原样**塞进模板（`"Err": strings.TrimSpace(c.Query("err"))`）：任何人手拼一个
// /admin/inventory?err=任意文案 就能在页面上塞一条顶着「上一次操作未完成」样式的伪造消息。
// 查询参数与响应体、模板数据一样**不是可信边界**。
//
// ?ok= / ?done= 此前是 token "1"（页面上会渲染成一个裸 "1"）——读侧按「成功态回显」
// 的约定把它**收敛成一句翻译过的固定文案**；其余取值一律过白名单，未命中落空串。

// inventoryNoticeToken 写侧回执里的固定成功 token（?ok=1 / ?done=1）。
const inventoryNoticeToken = "1"

// inventoryActionDoneKey / inventoryActionDoneFallback 成功回执的 i18n key 与中文兜底。
//
// 没有单独为它写迁移：词条缺失时 TranslateFunc 回落这份中文（与仓库里其它
// 「key + 中文兜底」的写法一致），中文环境的表现就是「操作已完成」，
// 而不是此前那个裸 "1"。
const (
	inventoryActionDoneKey      = "admin.inventory.actionDone"
	inventoryActionDoneFallback = "操作已完成"
)

// inventoryBulkNoticeTemplate 批量结论文案的一条模板（i18n key + 中文兜底）。
//
// 本批把它从匿名结构体提成具名类型，是为了让**写侧与读侧共用同一个取法**
// （inventoryBulkText）：此前读侧走 tr(tpl.key, tpl.fallback)、写侧各写各的，
// 货源页（inventory_source_page_handle.go）干脆还是硬编码中文。
type inventoryBulkNoticeTemplate struct {
	key, fallback string
	// strict 只允许 %s 占位符：词条被写坏时回落中文兜底。
	//
	// 仓库的两条是迁移 243 已落库的词条（中英都用 %d），保持宽松路径 = 与旧行为逐字一致；
	// **新增**的词条一律 strict（%s + strconv.Itoa），因为 %d 会让「占位符个数写错」
	// 在 Sprintf 时静默产出 %!d(MISSING) 之类的东西，而这条路直接给运营看。
	strict bool
}

// inventoryBulkText 取一条批量结论文案的当前语言模板（写侧与读侧**共用这一个取法**）。
func inventoryBulkText(c *gin.Context, t inventoryBulkNoticeTemplate) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if t.strict && !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// inventoryBulkNoticeTemplates 批量操作的结论文案模板（写侧与读侧共用同一份）。
//
// 写侧各 BulkDelete 用 inventoryBulkText 取词后 Sprintf；读侧 inventoryNoticeTexts 从
// **同一张表**取同一条词条再归一比对（数字归一后相等）。两处若各写一份字面量，
// 改词条时读侧会静默失配（提示在写侧可见、到了页面上变成归口文案）。
var (
	inventoryBulkWarehousePartial = inventoryBulkNoticeTemplate{"admin.inventory.bulk.partial", "已删除 %d 个，%d 个未能删除（默认仓或仓内仍有非零库存）", false}
	inventoryBulkWarehouseDone    = inventoryBulkNoticeTemplate{"admin.inventory.bulk.deleted", "已删除 %d 个仓库", false}
	inventoryBulkSourcePartial    = inventoryBulkNoticeTemplate{"admin.inventory.bulk.sourcePartial", "已删除 %s 个，%s 个未能删除（仍被采购单或历史流水引用）", true}
	inventoryBulkSourceDone       = inventoryBulkNoticeTemplate{"admin.inventory.bulk.sourceDone", "已删除 %s 个货源", true}

	inventoryBulkNoticeTemplates = []inventoryBulkNoticeTemplate{
		inventoryBulkWarehousePartial, inventoryBulkWarehouseDone,
		inventoryBulkSourcePartial, inventoryBulkSourceDone,
	}
)

// inventoryNoticeTexts 库存模块各页面可以原样展示的回执文案（当前语言）。
func inventoryNoticeTexts(c *gin.Context) []string {
	tr := shell.TranslateFor(c)
	out := make([]string, 0, len(inventoryErrFallbacks)*4+len(inventoryBulkNoticeTemplates)+3)
	// 键集取 inventoryErrKeys（inventoryErrKey 的判据），而不是兜底表 ——
	// 写侧只会产出「keys 表内 key」的译文与兜底，两处共用同一份来源才不会漏。
	for key := range inventoryErrKeys {
		fallback := inventoryErrFallback(key)
		// 三种形态都收：key（i18n 未初始化时的取值）、中文兜底（inventoryErrFallback 的产物）、
		// 当前语言译文（inventoryErrText 的正常产物）。
		out = append(out, key, fallback, tr(key, fallback), tr(key, key))
	}
	out = append(out,
		tr(shell.MsgInternalError, inventoryErrInternalFallback),
		shell.BulkIDsNoticeTemplate(c),
	)
	for _, tpl := range inventoryBulkNoticeTemplates {
		out = append(out, shell.NoticeTemplate(inventoryBulkText(c, tpl)))
	}
	return out
}

// inventoryPageErr 库存各页 ?err= 的统一出口（未命中落归口文案）。
func inventoryPageErr(c *gin.Context) string {
	return shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), func(raw string) string {
		return shell.FacingNotice(raw, inventoryNoticeTexts(c))
	})
}

// inventoryPageOk 库存各页 ?ok= 的统一出口。
func inventoryPageOk(c *gin.Context) string { return inventoryNoticeSuccess(c, c.Query("ok")) }

// inventoryPageDone 库存各页 ?done= 的统一出口。
func inventoryPageDone(c *gin.Context) string { return inventoryNoticeSuccess(c, c.Query("done")) }

// inventoryNoticeSuccess 成功回执的读侧出口：固定 token → 翻译后的固定文案；其余过白名单。
func inventoryNoticeSuccess(c *gin.Context, raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if msg == inventoryNoticeToken {
		return shell.TranslateFor(c)(inventoryActionDoneKey, inventoryActionDoneFallback)
	}
	return shell.FacingNotice(msg, inventoryNoticeTexts(c))
}

// inventoryErrFallbacks 兜底文案（i18n 未初始化 / 该 key 还没有词条时用）。
//
// 它不是「第二份真相」：已 seed 的 key 在真实请求里走 sys_i18n，这里只在取不到词条时生效。
// 登记范围是**页面直接铺出来的那几条**（外码行内编辑 / 跟踪与数量 / 采购收货与生产入库），
// 不是「把 enums 全抄一遍」—— 全抄会与迁移里的 seed 分叉（改了 seed 忘改这里，
// 桌面站点与英文站点就会显示两句不一样的话）；而漏登记的方向更糟：
// inventoryErrFallback 会返回 key 本身，页面上出现 ErrReceiptOrderDone 这种裸 key。
var inventoryErrFallbacks = map[string]string{
	inventoryenums.ErrExternalSKUInvalid:         "外部编码不合法：长度需在 128 个字符以内且不含控制字符",
	inventoryenums.ErrExternalSKUProductConflict: "该外部编码在本仓已挂在另一个商品上：同一个外部编码在同一仓库内只能属于同一个商品（同一商品的多个口味可以共用一个外部编码）",
	// 无限库存行内编辑（迁移 261）直接暴露的两条（同上：只登记页面直接铺出来的那几条，
	// 不在兜底表里重抄全部 key —— 那会与迁移里的 seed 分叉）。
	inventoryenums.ErrStockQuantityRequired:  "跟踪库存时必须填写数量（0 表示已卖光，需要自己填出来，留空不等于 0）",
	inventoryenums.ErrStockUntrackedQuantity: "不跟踪（无限）的库存行不允许带数量：要写具体数量请先切换成跟踪库存",
	// 入库入口的 SKU 编码（迁移 270 已 seed 中英词条）：登记兜底是给
	// 「i18n 未初始化 / 取词失败」那条降级路径用的，正常情况下仍走 sys_i18n。
	inventoryenums.ErrStockSKURequired: "缺少仓库侧 SKU 编码：请传裸码（不带仓码前缀）",
	// —— 采购单 / 收货 / 生产入库（issue #18）——
	//
	// 这一组是**采购页三个写入口直接铺出来的**业务错误（建单 / 收货 / 生产入库），
	// 未登记时 inventoryErrFallback 会返回 key 本身 —— 页面上出现的是 ErrReceiptOrderDone
	// 这种裸 key，既不中文也不是一句人能读的话。本批把采购页的错误回显从 err.Error()
	// 收口到 inventoryErrText（见 redirectPurchaseErr），兜底表同步补齐。
	// 这批 key 的 sys_i18n 词条**尚未 seed**（242/243 收的是仓库 / 库存 / 原因三类），
	// 所以兜底文案就是用户实际看到的那句话；词条落地后由词条接管，这里仍是降级路径。
	inventoryenums.ErrSourceNotFound:              "货源不存在",
	inventoryenums.ErrPurchaseOrderNotFound:       "采购单不存在",
	inventoryenums.ErrPurchaseCodeRequired:        "采购单号必填",
	inventoryenums.ErrPurchaseCodeInvalid:         "采购单号只允许大写字母 / 数字 / 下划线 / 连字符，且不超长",
	inventoryenums.ErrPurchaseCodeTaken:           "同工程下采购单号已被占用",
	inventoryenums.ErrPurchaseSourceRequired:      "采购单必须指定货源",
	inventoryenums.ErrPurchaseSourceDisabled:      "已停用的货源不能下采购单（停用 = 不再选用）",
	inventoryenums.ErrPurchaseLinesRequired:       "采购单至少要有一行",
	inventoryenums.ErrPurchaseLinesTooMany:        "采购行数超过上限",
	inventoryenums.ErrPurchaseLineRequired:        "采购行必须给出变体",
	inventoryenums.ErrPurchaseLineDuplicate:       "同一采购单里同一个 SKU 重复出现",
	inventoryenums.ErrPurchaseQuantityInvalid:     "采购数量必须为正整数",
	inventoryenums.ErrPurchasePriceInvalid:        "采购单价必须为正数",
	inventoryenums.ErrPurchaseStatusInvalid:       "状态筛选值不是未入库 / 部分入库 / 已入库",
	inventoryenums.ErrPurchaseLinesLocked:         "已有入库数量的采购单不能再改采购行（改了状态推导就不成立）",
	inventoryenums.ErrPurchaseLineNotFound:        "采购行不存在（或不属于该采购单）",
	inventoryenums.ErrReceiptLinesRequired:        "入库清单不能为空",
	inventoryenums.ErrReceiptLineDuplicate:        "同一入库请求里同一采购行只能出现一次",
	inventoryenums.ErrReceiptQuantityInvalid:      "入库数量必须为正整数",
	inventoryenums.ErrReceiptOverReceive:          "入库数量超过「采购数量 - 已入库数量」：超收不被接受",
	inventoryenums.ErrReceiptOrderDone:            "采购单已全部入库，无需再收",
	inventoryenums.ErrReceiptRequestInvalid:       "幂等键不合法（超长 / 非法字符）",
	inventoryenums.ErrProductionSourceNotInternal: "生产入库只认内部货源（自家工厂 / 集团内关联公司）：外部供应商走采购单",
	inventoryenums.ErrProductionVariantRequired:   "生产入库必须给出 SKU 变体",
	inventoryenums.ErrProductionCostInvalid:       "生产入库的成本价必须手工填写且非负",
}

// inventoryErrFallback 取兜底文案；没有登记时退回 key（至少还能与 sys_i18n 里的词条对照）。
func inventoryErrFallback(key string) string {
	if text, ok := inventoryErrFallbacks[key]; ok {
		return text
	}
	return key
}

// inventoryErrKeys 库存域业务错误的**文案 key 白名单**（enums 常量值）。
//
// 为什么是字符串白名单而不是 sentinel 列表：本模块的 enums 是**字符串常量**
// （值就是 i18n key），service 用 errors.New(...) 现场构造 error —— 每条都是新实例，
// errors.Is 认不出来。这里按 Error() 匹配 key，两种形态都认：
//
//	· 整串等于 key（errors.New(key)）；
//	· 以「key：」开头（service 用 fmt.Errorf("%s：补充说明", key) 把上下文跟在后面，
//	  如 N:1 冲突会把占位它的商品 id 带上）—— 取 key 部分查词条，补充说明拼在文案后。
//
// 被 fmt.Errorf("...: %w") 包过的错误（前缀在 key **之前**）仍匹配不上，会落到通用提示：
// 那种包装说明错误不是本模块主动抛给用户的业务判定，对外不该泄漏内部细节。
//
// 新增业务错误时在此同步登记：漏登记的后果是它被当成内部故障（通用提示），
// 用户拿到的是不可行动的提示，而页面上看不到任何内部细节。
var inventoryErrKeys = map[string]bool{
	inventoryenums.ErrInvalidParam: true,
	// 仓库
	inventoryenums.ErrWarehouseNotFound:             true,
	inventoryenums.ErrWarehouseNameRequired:         true,
	inventoryenums.ErrWarehouseCodeRequired:         true,
	inventoryenums.ErrWarehouseCodeInvalid:          true,
	inventoryenums.ErrWarehouseCodeTaken:            true,
	inventoryenums.ErrWarehouseStatusInvalid:        true,
	inventoryenums.ErrWarehouseIsDefault:            true,
	inventoryenums.ErrWarehouseHasStock:             true,
	inventoryenums.ErrWarehouseDisabled:             true,
	inventoryenums.ErrWarehouseProjectMismatch:      true,
	inventoryenums.ErrWarehouseDefaultMissing:       true,
	inventoryenums.ErrWarehouseTypeInvalid:          true,
	inventoryenums.ErrWarehouseConfigInvalid:        true,
	inventoryenums.ErrWarehouseCredentialInvalid:    true,
	inventoryenums.ErrWarehouseCredentialKeyMissing: true,
	inventoryenums.ErrWarehouseTypeVirtualDefault:   true,
	// 库存记录与变动
	inventoryenums.ErrStockNotFound:            true,
	inventoryenums.ErrStockVariantRequired:     true,
	inventoryenums.ErrStockWarehouseNeeded:     true,
	inventoryenums.ErrStockLinesRequired:       true,
	inventoryenums.ErrStockLinesTooMany:        true,
	inventoryenums.ErrStockDirectionInvalid:    true,
	inventoryenums.ErrStockQuantityInvalid:     true,
	inventoryenums.ErrStockProductRequired:     true,
	inventoryenums.ErrStockReasonRequired:      true,
	inventoryenums.ErrStockInsufficient:        true,
	inventoryenums.ErrStockRemarkRequired:      true,
	inventoryenums.ErrStockCostInvalid:         true, // 显式传入的成本价不合法（负数 / NaN）
	inventoryenums.ErrMovementTimeRangeInvalid: true,
	// 无限库存（迁移 261）：跟踪态必须显式给数量 / 不跟踪的行不允许带数量。
	// 不登记 = 被当成内部故障，用户只拿到「系统内部错误」，而这两条本来就是
	// 「表单该怎么填」的提示。
	inventoryenums.ErrStockQuantityRequired:  true,
	inventoryenums.ErrStockUntrackedQuantity: true,
	// 变动原因
	inventoryenums.ErrReasonNotFound:          true,
	inventoryenums.ErrReasonDirectionMismatch: true,
	inventoryenums.ErrReasonCodeRequired:      true,
	inventoryenums.ErrReasonCodeInvalid:       true,
	inventoryenums.ErrReasonCodeTaken:         true,
	inventoryenums.ErrReasonNameRequired:      true,
	inventoryenums.ErrReasonDirectionInvalid:  true,
	inventoryenums.ErrReasonBuiltin:           true,
	inventoryenums.ErrReasonStatusInvalid:     true,
	// 仓库 SKU 外部编码（迁移 251 / 词条 252）：库存页行内编辑外码的两条业务错误。
	// 不登记 = 被当成内部故障，用户只拿到「系统内部错误」，而「编码太长」这种
	// 本来就该自己改一下的事情，页面上看不到任何可行动的线索。
	inventoryenums.ErrExternalSKUInvalid:         true,
	inventoryenums.ErrExternalSKUProductConflict: true,
	// 入库入口的 SKU 编码校验（迁移 270 已 seed 词条）：空串一律拒绝，
	// 文案必须可行动（让调用方传裸码），不能退化成「系统内部错误」。
	inventoryenums.ErrStockSKURequired: true,
	// —— 采购单 / 收货 / 生产入库（issue #18）——
	//
	// 采购页三个写入口（建单 / 收货 / 生产入库）与它们取数失败的文案都经
	// redirectPurchaseErr / purchaseOrderRows → inventoryErrText 回显，因此这一组
	// 必须在本白名单里：漏登记 = 被当成内部故障，用户拿到「系统内部错误」而看不到
	// 任何可行动的线索（例如「超收了」「单已收满」「货源停用了」）。
	// ErrSourceNotFound / ErrPurchaseSourceDisabled 也在其中：resolvePurchaseSource
	// 的拒绝理由就是它们，属于采购路径的正常业务分支。
	inventoryenums.ErrSourceNotFound:              true,
	inventoryenums.ErrPurchaseOrderNotFound:       true,
	inventoryenums.ErrPurchaseCodeRequired:        true,
	inventoryenums.ErrPurchaseCodeInvalid:         true,
	inventoryenums.ErrPurchaseCodeTaken:           true,
	inventoryenums.ErrPurchaseSourceRequired:      true,
	inventoryenums.ErrPurchaseSourceDisabled:      true,
	inventoryenums.ErrPurchaseLinesRequired:       true,
	inventoryenums.ErrPurchaseLinesTooMany:        true,
	inventoryenums.ErrPurchaseLineRequired:        true,
	inventoryenums.ErrPurchaseLineDuplicate:       true,
	inventoryenums.ErrPurchaseQuantityInvalid:     true,
	inventoryenums.ErrPurchasePriceInvalid:        true,
	inventoryenums.ErrPurchaseStatusInvalid:       true,
	inventoryenums.ErrPurchaseLinesLocked:         true,
	inventoryenums.ErrPurchaseLineNotFound:        true,
	inventoryenums.ErrReceiptLinesRequired:        true,
	inventoryenums.ErrReceiptLineDuplicate:        true,
	inventoryenums.ErrReceiptQuantityInvalid:      true,
	inventoryenums.ErrReceiptOverReceive:          true,
	inventoryenums.ErrReceiptOrderDone:            true,
	inventoryenums.ErrReceiptRequestInvalid:       true,
	inventoryenums.ErrProductionSourceNotInternal: true,
	inventoryenums.ErrProductionVariantRequired:   true,
	inventoryenums.ErrProductionCostInvalid:       true,
}

// urlQueryEscape 查询参数转义（其它后台页 handler 回跳时拼 SKU / 关键字用）。
func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}

// inventoryErrKey 业务错误 → i18n 词条 key 与其补充说明（非业务错误返回空串，按内部故障处理）。
//
// 识别两种形态（见 inventoryErrKeys 的注释）：整串等于 key，或以「key：」开头。
func inventoryErrKey(err error) (key, tail string) {
	if err == nil {
		return "", ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "", ""
	}
	if inventoryErrKeys[msg] {
		return msg, ""
	}
	for candidate := range inventoryErrKeys {
		if strings.HasPrefix(msg, candidate+"：") {
			return candidate, strings.TrimSpace(strings.TrimPrefix(msg, candidate+"："))
		}
	}
	return "", ""
}
