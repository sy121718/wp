package inventoryhttp

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
// 契约、product 契约与 project 契约；GET 渲染完整页，写动作的结论由 shell.RenderJump
// 渲染成整页提示（原生表单 + csrf_token 隐藏域，见 inventory_jump.go）。
//
// 错误回显（硬规则）：**禁止把 err.Error() 铺到页面上** —— 业务错误的 Error() 就是
// enums 常量（也就是 i18n key），直接铺出去页面上会出现 ErrWarehouseCodeTaken 这种裸 key。
// 统一走 inventoryErrText：按请求语言取词、以 key 作兜底；非业务错误只给通用提示
// （与 internal/module/block/inbound/http/block_page_handle.go 的 blockErrText 同形）。

// 与库存管理页 / 货源管理页同一模式：GET 渲染完整页，写动作的结论由 shell.RenderJump
// 渲染成整页提示（原生表单 + csrf_token 隐藏域；文案经 inventoryErrText 取词后再交给提示页）。
//
// 页面承担六条验收：
//
//	· 验收 1：建采购单与采购行（SKU、数量、单价），行上的「登记入库」按钮提交后
//	  已入库数量累加（服务端在行锁内原子递增）；
//	· 验收 2：单据状态直接读服务端推导出的 pending / partial / received；
//	· 验收 3：登记入库即增加库存并写流水（原因是字典里的采购入库）；
//	· 验收 4：入库以采购单价更新 SKU 成本价（表单可填「本次到货价」，缺省用采购单价）；
//	· 验收 5：自家工厂生产入库表单 —— 无采购单、来源只列内部货源、成本价手工填写
//	  （表单与路由 /admin/inventory/purchases/production、权限点 inventory:purchase_production
//	  一直在，本批把页面上缺失的表单补回，见模板 inventory_purchases.html）；
//	· 验收 6：按 SKU 查进货历史（单据 / 数量 / 单价 / 来源 / 仓库 / 时间）——
//	  已并入库存管理页的流水筛选（按 SKU / 原因 / 时间），本页不再挂那张表。
//
// 幂等：每个写表单渲染时带一个一次性 requestId（隐藏域），双击提交只会产生一张入库单；
// 收货与生产入库**各带一个键** —— 幂等键在入库单上全局唯一，共用一个键会让
// 「先收采购货、再做生产入库」的第二跳被当成同一张单的重放而静默丢弃。

// 与库存管理页同一模式：GET 渲染完整页，写动作的结论由 shell.RenderJump 渲染成整页提示
// （原生表单 + csrf_token 隐藏域，见 inventory_jump.go）。
//
// 页面承担四条验收：
//
//	· 验收 1：可建货源，类型区分内部与外部，可标记关联方；
//	· 验收 2：货源可配置异构的对接扩展信息（config，JSON 对象）；
//	· 验收 3：后台可管理货源（建 / 改 / 停启用 / 删 / 筛选）；
//	· 验收 4：关联方标志可用于报表区分 —— 顶部给出「类型 × 关联方」的交叉统计，
//	  筛选栏的关联方维度直接落到查询上。
//
// 表单提交语义：这一页的编辑表单就是**最终状态**（字段全部回填），
// 因此「结算价留空」= 清空结算价，而不是「本次不改」。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/inventory/contract"
	"go_wp/internal/module/inventory/dto"
	"go_wp/internal/module/inventory/enums"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// 页面路径常量（回跳地址的唯一定义处，避免各 handler 手写字符串抄错）。
const (
	inventoryPagePath         = "/admin/inventory"
	inventoryWarehousesPath   = "/admin/inventory/warehouses"
	inventoryReasonsPath      = "/admin/inventory/reasons"
	inventorySourcesPath      = "/admin/inventory/sources"
	inventoryMovementPageSize = 50
	// inventoryMovementMaxPageSize 流水页每页条数的上限（?limit= 的封顶值）。
	//
	// 与 service 的 movementMaxPageSize 同口径：超过它的 size 会被 service 自己截到 200，
	// 页面若允许更大的值，URL 上写着 500 而实际只回 200 —— 表现为「翻页少一截」，
	// 这类缺陷最难查（页面不报错，只是数据对不上）。所以在入口就按同一个数字封顶。
	inventoryMovementMaxPageSize = 200
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
					"QuantityLabel": tr(inventoryenums.InventoryStockNotStocked, "未入库"),
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
	// 分页：?page=&limit=，缺省每页 50 条（inventoryMovementPageSize）。
	page, limit := inventoryPageParams(c, inventoryMovementPageSize, inventoryMovementMaxPageSize)
	movements, total, page, err := h.listMovements(c, selected, sku, filterWarehouse, filterDirection,
		filterReason, filterTimeFrom, filterTimeTo, page, limit)
	if err != nil {
		shell.PageError(c, "inventory", err)
		return
	}

	tr := shell.TranslateFor(c)
	pageData := gin.H{
		"title":            tr(inventoryenums.InventoryTitle, "库存管理"),
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
		"Directions":       directionOptions(tr),
		"Movements":        movements,
		"FilterWarehouse":  filterWarehouse,
		"FilterDirection":  filterDirection,
		"FilterReason":     filterReason,
		"FilterTimeFrom":   filterTimeFrom,
		"FilterTimeTo":     filterTimeTo,
		// 写动作表单的 action 上带的筛选上下文（服务端渲染时拼、POST 回来由 shell.BackPath
		// 读回）：写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 inventory_jump.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventoryStockBackKeys...),
	}
	// 分页条（shell 组件，服务端渲染）：基址带当前全部筛选维度（工程 / SKU / 变体 / 仓 /
	// 方向 / 原因 / 时间区间），翻页时不丢条件 —— 丢了条件会让人以为「记录变多了」，
	// 实际是筛选被清掉。
	// total 来自契约的 CountMovements（与 ListMovements 同一份过滤条件），因此分页条给的是
	// 真页码窗口与「共 N 条」，而不是「上一页 / 下一页 + 后面还有记录」这种探测式降级形态。
	// 单页或空数据时 BuildPagination 返回 nil，TemplateKeys 给空 map，模板自然不渲染。
	for k, v := range shell.BuildPagination(total, page, limit, shell.FilterBaseURL(inventoryPagePath,
		map[string]string{
			"project":     selected,
			"sku":         sku,
			"variantId":   variantID,
			"warehouseId": filterWarehouse,
			"direction":   filterDirection,
			"reasonCode":  filterReason,
			"timeFrom":    filterTimeFrom,
			"timeTo":      filterTimeTo,
		}), shell.TranslateFor(c)).TemplateKeys() {
		pageData[k] = v
	}
	c.HTML(http.StatusOK, "admin/inventory/inventory.html", shell.Prepare(c, pageData))
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
	tr := shell.TranslateFor(c)
	c.HTML(http.StatusOK, "admin/inventory/inventory_warehouses.html", shell.Prepare(c, gin.H{
		"title":               tr(inventoryenums.InventoryWarehousesTitle, "仓库管理"),
		"menu":                "inventory-warehouses",
		"Projects":            projects,
		"SelectedProject":     selected,
		"Warehouses":          warehouses,
		"WarehouseTypes":      warehouseTypeOptions(c),
		"WarehouseCreateForm": warehouseFormData(c, false, nil),
		// 写动作表单 action 上带的筛选上下文（见 inventory_jump.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventoryWarehouseBackKeys...),
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
	tr := shell.TranslateFor(c)
	c.HTML(http.StatusOK, "admin/inventory/inventory_reasons.html", shell.Prepare(c, gin.H{
		"title":            tr(inventoryenums.InventoryReasonsTitle, "变动原因字典"),
		"menu":             "inventory-reasons",
		"Projects":         projects,
		"SelectedProject":  selected,
		"Reasons":          reasons,
		"Directions":       directionOptions(tr),
		"ReasonCreateForm": reasonFormData(c),
		// 写动作表单 action 上带的筛选上下文（见 inventory_jump.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventoryReasonBackKeys...),
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
		h.warehouseFormFail(c, false, err)
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryWarehouseBack(c), inventoryWarehouseBackText(c))
}

// InventoryWarehouseUpdate 修改仓库（名称 / 短码 / 类型 / 排序 / 状态 / 第三方对接配置）。
func (h *inventoryPageHandle) InventoryWarehouseUpdate(c *gin.Context) {
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
		h.warehouseFormFail(c, true, err)
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryWarehouseBack(c), inventoryWarehouseBackText(c))
}

// InventoryWarehouseDefault 切换默认仓（同工程唯一；「未指定仓库」的兜底）。
func (h *inventoryPageHandle) InventoryWarehouseDefault(c *gin.Context) {
	yes := true
	req := &inventorydto.UpdateWarehouseReq{ID: c.PostForm("id"), IsDefault: &yes}
	if _, err := h.inventory.UpdateWarehouse(c.Request.Context(), req); err != nil {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryWarehouseBack(c), inventoryWarehouseBackText(c))
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryWarehouseBack(c), inventoryWarehouseBackText(c))
}

// InventoryWarehouseDelete 删除仓库（默认仓 / 有非零库存时服务端拒绝）。
func (h *inventoryPageHandle) InventoryWarehouseDelete(c *gin.Context) {
	if err := h.inventory.DeleteWarehouse(c.Request.Context(),
		&inventorydto.DeleteWarehouseReq{ID: c.PostForm("id"), ProjectID: c.PostForm("projectId")}); err != nil {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryWarehouseBack(c), inventoryWarehouseBackText(c))
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryWarehouseBack(c), inventoryWarehouseBackText(c))
}

// InventoryWarehousesBulkDelete 批量删除仓库。
//
// 逐条走同一条删除路径：默认仓、仓内仍有非零库存的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染成提示页，避免静默的部分成功（语义未变，只换传输通道）。
func (h *inventoryPageHandle) InventoryWarehousesBulkDelete(c *gin.Context) {
	projectID := c.PostForm("projectId")
	back := inventoryWarehouseBack(c)
	backText := inventoryWarehouseBackText(c)
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是 shell 的受控错误（值域只有 Count/Max）：走它的受控文案出口，
		// 而不是把 err.Error() 拼进提示页（提示页不是可信边界，文案必须已归口）。
		inventoryJump(c, false, shell.BulkIDsFacingText(c, berr), back, backText)
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
		inventoryJump(c, false, fmt.Sprintf(inventoryBulkText(c, inventoryBulkWarehousePartial), deleted, skipped), back, backText)
	case deleted > 0:
		inventoryJump(c, true, fmt.Sprintf(inventoryBulkText(c, inventoryBulkWarehouseDone), deleted), back, backText)
	default:
		// 没有选中任何 id（批量条在无勾选时不会提交）：回列表，不渲染提示页 ——
		// 与改造前一致，且提示页不能显示空串（RenderJump 会把空串落成归口文案）。
		redirectWhere(c, back)
	}
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
		if err != nil {
			inventoryJump(c, false, inventoryErrText(c, err), inventoryStockBack(c), inventoryStockBackText(c))
			return
		}
		inventoryJump(c, true, inventoryDoneText(c), inventoryStockBack(c), inventoryStockBackText(c))
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
// 权限：复用库存页既有写入口的权限点（inventory:stock_change，见 inventory_page_router.go），
// 不新增权限点、不新增 authorizedAPI 路由。
func (h *inventoryPageHandle) InventoryExternalSKUUpdate(c *gin.Context) {
	req := &inventorydto.BindExternalSKUReq{
		ProjectID:   c.PostForm("projectId"),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		VariantID:   strings.TrimSpace(c.PostForm("variantId")),
		// 这里不做任何加工：归一（去首尾空白 / 长度上限 / 控制字符）是 service 的唯一口径。
		ExternalSKU: c.PostForm("externalSku"),
	}
	if _, err := h.inventory.BindExternalSKU(c.Request.Context(), req); err != nil {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryStockBack(c), inventoryStockBackText(c))
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryStockBack(c), inventoryStockBackText(c))
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
// 权限：复用库存页既有写入口的权限点（inventory:stock_change，见 inventory_page_router.go），
// 不新增权限点、不新增 authorizedAPI 路由。
func (h *inventoryPageHandle) InventoryStockTrackingUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	track := c.PostForm("trackQuantity") != ""
	rawQuantity := strings.TrimSpace(c.PostForm("quantity"))

	respond := func(err error) {
		if err != nil {
			inventoryJump(c, false, inventoryErrText(c, err), inventoryStockBack(c), inventoryStockBackText(c))
			return
		}
		inventoryJump(c, true, inventoryDoneText(c), inventoryStockBack(c), inventoryStockBackText(c))
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
		h.reasonFormFail(c, err)
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryReasonBack(c), inventoryReasonBackText(c))
}

// InventoryReasonUpdate 后台表单修改自定义变动原因（改名 / 停用 / 排序）。
//
// 内置原因只读：改名一律由服务端拒绝（它的 key 由系统按 code 派生），仅允许停用 / 启用。
func (h *inventoryPageHandle) InventoryReasonUpdate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.UpdateReasonReq{ProjectID: projectID, ID: c.PostForm("id")}
	if rawName, submitted := c.GetPostForm("name"); submitted {
		name := strings.TrimSpace(rawName)
		req.Name = &name
	}
	if status := strings.TrimSpace(c.PostForm("status")); status != "" {
		req.Status = &status
	}
	if rawSort := strings.TrimSpace(c.PostForm("sort")); rawSort != "" {
		sortValue := parseIntOr(rawSort, 0)
		req.Sort = &sortValue
	}
	if _, err := h.inventory.UpdateReason(c.Request.Context(), req); err != nil {
		h.reasonEditFormFail(c, err)
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryReasonBack(c), inventoryReasonBackText(c))
}

// InventoryReasonsBulkStatus 按现有更新权限逐条切换原因状态，不触碰名称和排序。
func (h *inventoryPageHandle) InventoryReasonsBulkStatus(c *gin.Context) {
	projectID := c.PostForm("projectId")
	back := inventoryReasonBack(c)
	backText := inventoryReasonBackText(c)
	status := strings.TrimSpace(c.PostForm("status"))
	if status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
		inventoryJump(c, false, inventoryErrText(c, errors.New(inventoryenums.ErrReasonStatusInvalid)), back, backText)
		return
	}
	ids, err := shell.BulkIDs(c)
	if err != nil {
		inventoryJump(c, false, shell.BulkIDsFacingText(c, err), back, backText)
		return
	}
	if len(ids) == 0 {
		inventoryJump(c, false, inventoryBulkText(c, inventoryBulkReasonNoneSelected), back, backText)
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
	// 模板是 %s（strict 校验只认字符串占位符），所以数字先转成字符串再填 ——
	// 传 int 会让 Sprintf 产出 `%!s(int=2)` 这种垃圾，而它看起来「像一句结论」。
	switch {
	case skipped > 0:
		inventoryJump(c, false, fmt.Sprintf(inventoryBulkText(c, inventoryBulkReasonPartial), strconv.Itoa(updated), strconv.Itoa(skipped)), back, backText)
	case updated > 0:
		inventoryJump(c, true, fmt.Sprintf(inventoryBulkText(c, inventoryBulkReasonDone), strconv.Itoa(updated)), back, backText)
	default:
		redirectWhere(c, back)
	}
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
			"StatusLabel":      statusLabel(tr, w.Status),
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
			"Direction": r.Direction, "DirectionLabel": directionLabel(tr, r.Direction),
			"IsBuiltin": r.IsBuiltin, "Status": r.Status, "Sort": r.Sort,
			"StatusLabel": statusLabel(tr, r.Status),
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

// listMovements 库存流水列表（按 SKU / 仓库 / 方向 / 原因 / 时间过滤 + 分页）。
//
// total 是该过滤条件下的**真实总条数**（契约的 CountMovements，与 ListMovements 同一份
// 过滤条件）；curPage 是**收敛后**的页码（page 越界时回落到末页）。out 只装本页数据。
func (h *inventoryPageHandle) listMovements(c *gin.Context, projectID, sku, warehouseID,
	direction, reasonCode, timeFrom, timeTo string, page, limit int) (out []gin.H, total int64, curPage int, err error) {
	ctx := c.Request.Context()
	out = []gin.H{}
	if projectID == "" {
		return out, 0, 1, nil
	}
	// 过滤条件只构造一次（profile），列表与计数各自复制、只加各自的 Page/Size：
	// 两处各写一份过滤条件时，日后新增一个筛选维度只改到列表那一侧，就会出现
	// 「共 N 条」与实际能翻出来的条数互相矛盾 —— 而这恰恰只在那一维筛选时才暴露。
	filterReq := &inventorydto.ListMovementReq{
		ProjectID: projectID, SKUCode: sku, WarehouseID: warehouseID,
		Direction: direction, ReasonCode: reasonCode,
		TimeFrom: timeFrom, TimeTo: timeTo,
	}
	// 先计数、收敛页码，再取当页数据（顺序不能反，见 clampInventoryPage）。
	total, err = h.inventory.CountMovements(ctx, filterReq)
	if err != nil {
		return nil, 0, page, err
	}
	curPage = clampInventoryPage(page, limit, total)
	listReq := *filterReq
	listReq.Page, listReq.Size = curPage, limit
	rows, err := h.inventory.ListMovements(ctx, &listReq)
	if err != nil {
		return nil, 0, curPage, err
	}
	tr := shell.TranslateFor(c)
	for _, m := range rows {
		out = append(out, gin.H{
			"ID": m.ID, "SKUCode": m.SKUCode, "WarehouseName": m.WarehouseName,
			"WarehouseCode": m.WarehouseCode, "Direction": m.Direction,
			"DirectionLabel": directionLabel(tr, m.Direction),
			"Quantity":       m.Quantity, "QuantityBefore": m.QuantityBefore, "QuantityAfter": m.QuantityAfter,
			"ReasonName": tr(m.ReasonName, m.ReasonCode), "ReasonCode": m.ReasonCode,
			"SourceType": m.SourceType, "SourceRef": m.SourceRef, "Remark": m.Remark,
			"OperatorID": m.OperatorID, "BatchID": m.BatchID, "CreatedAt": m.CreatedAt,
		})
	}
	return out, total, curPage, nil
}

// —— 列表分页（库存流水 / 货源 / 采购入库三页共用）——
//
// 三页原先都是「handler 里写死上限 + 模板里没有分页条」：流水 50 条、货源 200 条、
// 采购单 100 条，第 N+1 条起**静默消失**（流水按时间倒序，第 51 条之后的老记录永远看不到）。
//
// 这里接的是项目既有的分页设施（`partials/pagination.html` + shell.BuildPagination），
// 与其它列表页共用同一套模板与样式：链接是普通 GET 参数（?page=&limit=），点页码整页刷新，
// 不引入任何前端状态、不与抽屉脚本耦合。
//
// 三个页面各自调一次契约的 CountXxx 取真实总数，因此给的是页码窗口与「共 N 条」。
// 此前的降级形态（实探第 page+1 页判断「还有没有下一页」+ 只给上一页 / 下一页 + 信息行写
// 「后面还有记录」）已删除：实探每页多发一次查询，而且拿不到总数就永远给不出页码。

// inventoryPageParams 解析列表页分页参数（?page= / ?limit=）。
//
// limit 缺省用各页自己的一页条数，超过 maxSize 按 maxSize 封顶（与 service 的上限同口径）；
// 非数字 / 非正值一律回落 —— 后台页不因地址栏里一个脏参数而 500。
func inventoryPageParams(c *gin.Context, defaultSize, maxSize int) (page, limit int) {
	page = parseIntOr(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	limit = parseIntOr(c.Query("limit"), defaultSize)
	if limit < 1 {
		limit = defaultSize
	}
	if limit > maxSize {
		limit = maxSize
	}
	return page, limit
}

// clampInventoryPage 把页码收敛到实际总页数以内（total = 0 时收敛到第 1 页）。
//
// 必须在**取数之前**收敛：越界页码（手输 URL、过期书签、上一次筛选残留的 page）直接传给
// 列表接口时，服务端会老老实实返回一个空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」这种自相矛盾的组合就是这样来的
// （shell.BuildPagination 只收敛它自己显示的那一页，不会回头改取数用的页码）。
//
// 总数因此必须在取数之前拿到 —— 这三个页面的契约都提供了 CountXxx，本批起不再靠实探。
func clampInventoryPage(page, limit int, total int64) int {
	if limit < 1 {
		limit = inventoryMovementPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
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
func directionOptions(tr func(key, fallback string) string) []gin.H {
	return []gin.H{
		{"Value": inventoryenums.DirectionIn, "Label": directionLabel(tr, inventoryenums.DirectionIn)},
		{"Value": inventoryenums.DirectionOut, "Label": directionLabel(tr, inventoryenums.DirectionOut)},
		{"Value": inventoryenums.DirectionAdjust, "Label": directionLabel(tr, inventoryenums.DirectionAdjust)},
	}
}

// adjustDirectionOptions 库存调整入口可选的方向：盘点（adjust）与报损（out）。
//
// 文案走取词而不是硬编码：这是本批新增的用户可见文案，英文界面不该看到中文。
func adjustDirectionOptions(c *gin.Context) []gin.H {
	tr := shell.TranslateFor(c)
	return []gin.H{
		{"Value": inventoryenums.DirectionAdjust, "Label": tr(inventoryenums.InventoryAdjustDirectionAdjust, "盘点（填目标绝对量）")},
		{"Value": inventoryenums.DirectionOut, "Label": tr(inventoryenums.InventoryAdjustDirectionOut, "报损（填本次减少量）")},
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

// statusLabel 启停状态 → 当前语言展示文案（仓库 / 盘点原因 / 货源三处共用）。
//
// 三处原先各写一份**函数体逐字相同**的实现（warehouseStatusLabel / reasonStatusLabel /
// sourceStatusLabel）：判定用的常量不同（StatusDisabled / SourceStatusDisabled），但两者
// 的值同为 "disabled"，词条 key 与中文兜底完全一致 —— 输出等价，同一批展示文案只留一份定义。
//
// 为什么落 http 层而不是 enums：本模块 enums 的既定约定是「中文兜底留在调用点，不搬进
// enums」（见 inventory_text_keys.go 头部），本函数同时要 tr 回调与兜底文案，两者都只在
// 展示层存在；三个调用点又同属 inventoryhttp 包，无需跨包导出。
func statusLabel(tr func(key, fallback string) string, status string) string {
	if status == inventoryenums.StatusDisabled {
		return tr(inventoryenums.InventoryStatusDisabled, "已停用")
	}
	return tr(inventoryenums.InventoryStatusActive, "启用中")
}

// stockCostLabel 库存行的成本展示文案（(仓库, SKU) 的当前成本价，迁移 244）。
//
// 空值不能显示成 0：成本列可空表示**尚未核算**（还没核算过 / 由外部核算后导入），
// 而 0 是合法的显式成本（赠品 / 内部划拨）—— 两者混在一起，运营就再也分不清
// 「这个仓这条 SKU 没成本」和「这条 SKU 不要钱」。
func stockCostLabel(tr func(key, fallback string) string, cost *float64) string {
	if cost == nil {
		return tr(inventoryenums.InventoryCostUnknown, "未核算")
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
		return "∞ " + tr(inventoryenums.InventoryStockUnlimited, "无限")
	}
	return fmt.Sprintf("%d", quantity)
}

// —— 错误文案（回跳地址与提示页出口见 inventory_jump.go）——
//
// 原先这一节还有 inventoryURL / inventoryConclusion / inventoryErrURL 三个函数：写动作的
// 结论经 302 + `?err=` / `?ok=` / `?done=` 回列表页，回跳地址由 Go 拼。改成 shell.RenderJump
// 渲染提示页之后，**结论不再进 URL**，那套「拼 URL + 读侧白名单」整批删除。

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
		if detail := inventoryErrDetailText(tr, tail); detail != "" {
			// 补充说明原样跟在译文后：service 写进去的是「为什么被拒」的上下文
			//（例如被哪个商品占了外码），吞掉它就等于让人去猜。
			text += "：" + detail
		}
	}
	return text
}

// inventoryErrDetailText 业务错误的**补充说明** → 当前语言文案。
//
// 补充说明有两种形态：
//
//  1. i18n.ErrorDetail 的产物（控制字符开头的「明细词条 key + 具名参数」，可多段）——
//     service 用它把「为什么被拒」也词条化（例如「该外部编码在本仓已属于商品 X」），
//     整句按当前语言取词并填 {name} 占位符；
//  2. 其余（历史形态的纯文本）—— 原样透出，行为与本通道引入前一致。
//
// 未登记的明细 key 跳过该段并记一条日志（少一句补充说明，好过把编码串摆到页面上）。
func inventoryErrDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		return tail
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := inventoryenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(inventoryErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

// —— 结论文案（读侧回执已整批删除）——
//
// 写动作的结论不再经 `?err=` / `?ok=` / `?done=` 回列表页（改由 shell.RenderJump 在响应体里
// 渲染提示页，见 inventory_jump.go），所以下面这套「证明提示出自本仓」的读侧判定整批删除：
// inventoryPageErr / inventoryPageOk / inventoryPageDone / inventoryNoticeSuccess /
// inventoryNoticeTexts / inventoryNoticeToken / inventoryBulkNoticeTemplates。
//
// 留下的都是**写侧**要用的取法：结论文案模板与「key + 中文兜底」的取词。

// inventoryActionDoneKey / inventoryActionDoneFallback 单条写动作成功回执的 i18n key 与中文兜底。
//
// 词条见迁移 280（中英各一行）；取不到词条时回落这份中文（与仓库里其它
// 「key + 中文兜底」的写法一致），中文环境的表现就是「操作已完成」。
const (
	inventoryActionDoneKey      = "admin.inventory.actionDone"
	inventoryActionDoneFallback = "操作已完成"
)

// inventoryBulkNoticeTemplate 批量结论文案的一条模板（i18n key + 中文兜底）。
//
// 具名类型 + inventoryBulkText 统一取法：各 BulkDelete 写侧都从这里取词后再 Sprintf，
// 不各写各的字面量（改词条时只改一处）。
type inventoryBulkNoticeTemplate struct {
	key, fallback string
	// strict 只允许 %s 占位符：词条被写坏时回落中文兜底。
	//
	// 仓库的两条是迁移 243 已落库的词条（中英都用 %d），保持宽松路径 = 与旧行为逐字一致；
	// **新增**的词条一律 strict（%s + strconv.Itoa），因为 %d 会让「占位符个数写错」
	// 在 Sprintf 时静默产出 %!d(MISSING) 之类的东西，而这条路直接给运营看。
	strict bool
}

// inventoryBulkText 取一条批量结论文案的当前语言模板（写侧唯一取法）。
func inventoryBulkText(c *gin.Context, t inventoryBulkNoticeTemplate) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if t.strict && !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// inventoryBulkNoticeTemplates 批量操作的结论文案模板。
//
// 各 BulkDelete 写侧用 inventoryBulkText 取词后 Sprintf（%d / %s 的口径见各模板的 strict）。
var (
	inventoryBulkWarehousePartial   = inventoryBulkNoticeTemplate{"admin.inventory.bulk.partial", "已删除 %d 个，%d 个未能删除（默认仓或仓内仍有非零库存）", false}
	inventoryBulkWarehouseDone      = inventoryBulkNoticeTemplate{"admin.inventory.bulk.deleted", "已删除 %d 个仓库", false}
	inventoryBulkSourcePartial      = inventoryBulkNoticeTemplate{"admin.inventory.bulk.sourcePartial", "已删除 %s 个，%s 个未能删除（仍被采购单或历史流水引用）", true}
	inventoryBulkSourceDone         = inventoryBulkNoticeTemplate{"admin.inventory.bulk.sourceDone", "已删除 %s 个货源", true}
	inventoryBulkReasonPartial      = inventoryBulkNoticeTemplate{"admin.inventory.bulk.reasonPartial", "已更新 %s 个原因，%s 个未能更新", true}
	inventoryBulkReasonDone         = inventoryBulkNoticeTemplate{"admin.inventory.bulk.reasonDone", "已更新 %s 个原因", true}
	inventoryBulkReasonNoneSelected = inventoryBulkNoticeTemplate{"admin.inventory.bulk.reasonNoneSelected", "请选择要操作的原因", false}
)

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
	// 收口到 inventoryErrText，兜底表同步补齐。
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
	// purchaseOrderRows → inventoryErrText 回显，因此这一组
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

var warehouseCreateFields = []string{"projectId", "code", "name", "sort", "isDefault", "type", "provider", "externalCode", "address", "contact", "allowsShipping", "apiCredential", "secretRef"}
var warehouseEditFields = []string{"projectId", "id", "code", "name", "sort", "type", "provider", "externalCode", "address", "contact", "allowsShipping", "apiCredential", "secretRef", "status"}

func warehouseFormData(c *gin.Context, edit bool, row gin.H) gin.H {
	return gin.H{
		"IsEdit": edit, "Row": row, "SelectedProject": c.PostForm("projectId"),
		"WarehouseTypes": warehouseTypeOptions(c),
	}
}

func (h *inventoryPageHandle) warehouseFormFail(c *gin.Context, edit bool, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryWarehouseBack(c), inventoryWarehouseBackText(c))
		return
	}
	// htmx 分支原样保留：失败在抽屉里重渲片段（错误槽 + 回填后的表单），不丢用户输入。
	data := warehouseFormData(c, edit, nil)
	fields := warehouseCreateFields
	if edit {
		fields = warehouseEditFields
	}
	data["FormEcho"] = inventoryRawFormValues(c, fields)
	data["SubmitErr"] = inventoryErrText(c, err)
	data["ListQuery"] = inventoryQueryFromRequest(c, inventoryWarehouseBackKeys...)
	c.HTML(http.StatusOK, "admin/inventory/inventory_warehouse_form.html", shell.Prepare(c, data))
}

// 原值快照保留空串和首尾空白；业务请求归一化不影响重新编辑的输入。
func inventoryRawFormValues(c *gin.Context, fields []string) gin.H {
	values := make(gin.H, len(fields))
	for _, field := range fields {
		values[field] = c.PostForm(field)
	}
	return values
}

var reasonCreateFields = []string{"projectId", "code", "name", "direction", "sort"}
var reasonEditFields = []string{"projectId", "id", "builtin", "name", "status", "sort"}

func reasonFormData(c *gin.Context) gin.H {
	return gin.H{
		"SelectedProject": c.PostForm("projectId"),
		"Directions":      directionOptions(shell.TranslateFor(c)),
	}
}

func (h *inventoryPageHandle) reasonFormFail(c *gin.Context, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryReasonBack(c), inventoryReasonBackText(c))
		return
	}
	// htmx 分支原样保留：失败在抽屉里重渲片段。
	data := reasonFormData(c)
	data["FormEcho"] = inventoryRawFormValues(c, reasonCreateFields)
	data["SubmitErr"] = inventoryErrText(c, err)
	data["ListQuery"] = inventoryQueryFromRequest(c, inventoryReasonBackKeys...)
	c.HTML(http.StatusOK, "admin/inventory/inventory_reason_form.html", shell.Prepare(c, data))
}

func (h *inventoryPageHandle) reasonEditFormFail(c *gin.Context, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryReasonBack(c), inventoryReasonBackText(c))
		return
	}
	// htmx 分支原样保留：失败在抽屉里重渲片段。
	data := reasonFormData(c)
	data["Edit"] = true
	data["FormEcho"] = inventoryRawFormValues(c, reasonEditFields)
	data["SubmitErr"] = inventoryErrText(c, err)
	data["ListQuery"] = inventoryQueryFromRequest(c, inventoryReasonBackKeys...)
	c.HTML(http.StatusOK, "admin/inventory/inventory_reason_form.html", shell.Prepare(c, data))
}

// parseIntOr 解析十进制整数，失败返回兜底值（后台表单容错，不因一个脏字段 500）。
func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

// —— HTMX 写表单的「分档出口」（本包各写表单共用；本文件是这一档位的唯一实现处）——
//
// 背景：本包各页的写表单失败时原先是 `302 + ?err=` 回列表页，**用户刚填的内容全丢**。
// 改造方向是路径 A「渐进增强」：表单保留原生 `method="post" action=…`，另加
// `hx-post`（htmx 优先拦住 submit）；服务端按 `HX-Request` **分档**：
//
//	· htmx 请求：失败返 **200 + 片段**（错误槽 + 回填后的表单）—— 抽屉 / 页面原地留住输入；
//	· 原生请求：失败渲染整页提示（inventoryJump / shell.RenderJump，见 inventory_jump.go）——
//	  取代原先的 `302 + ?err=`，结论走响应体而不是 URL。
//
// 这是**另一个包**，不复用 producthttp 的同款 helper：两个页面包之间没有共享依赖，
// 为两个小函数引入跨模块 import 会把「商品页改了会不会影响库存页」变成新的耦合面。
// 两边语义刻意逐字一致，改动时请一起改（分档口径分叉会表现为同一种失败在商品页留输入、
// 在库存页丢输入）。
//
// 文案仍然走本包既有设施：失败片段里的错误槽用 **inventoryErrText**（业务错误取词 +
// 结构化日志，非业务错误给归口文案），不要另开一套错误文案 —— 分档只改「响应形状」，
// 不改「错误怎么变成人话」。

// isHXRequest 这次请求是否由 htmx 发起（HX-Request: true）。
//
// 判据委托 shell.IsHXRequest（同一份实现）：RenderJump 对 htmx 输出 HX-Redirect 用的也是它，
// 两个出口必须认同同一个信号 —— 各写一份就会出现「某个模块的失败分档静默失效」。
func isHXRequest(c *gin.Context) bool {
	return shell.IsHXRequest(c)
}

// redirectWhere 页面写动作的 PRG 出口。
//
// 原生表单走 302；HTMX 请求走 **HX-Redirect**：htmx 的 XHR 会自己跟随 302，最终响应里
// 已经读不到 Location，只有响应头上的 HX-Redirect 能让它整页跳转（否则会把整页 HTML
// 塞进抽屉里）。两条路的终点是同一个 URL，页面壳与提示位完全一致。
//
// 用法：把既有的 `c.Redirect(http.StatusFound, target)` 换成 `redirectWhere(c, target)` ——
// 原生行为逐字不变，htmx 那一档才有正确的跳转；**不要**给写表单配 `hx-swap="none"`
// 之外的 hx-* 目标（跳转由响应头决定，不由 swap 决定）。
//
// 写动作的**结论**出口是 inventoryJump（shell.RenderJump，见 inventory_jump.go）：
// 这里保留 redirectWhere 只为「没有选中任何 id」这类无提示的回列表分支。
func redirectWhere(c *gin.Context, target string) {
	if isHXRequest(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, target)
}

// hxFragment 只在 htmx 请求下渲染片段（200 + HTML），并报告「是否已经响应」。
//
// 用法（写表单失败分档的唯一写法）：
//
//	if hxFragment(c, "admin/<模块>/xxx_form.html", data) {   // 模块专属片段跟模块走（见 templates/CLAUDE.md）
//		return
//	}
//	inventoryJump(c, false, inventoryErrText(c, err), back, backText)
//
// 片段模板必须**可独立渲染**（不带 extends layout，data 由调用点给齐），并且自带
// htmx 需要的结构（错误槽 + 回填后的表单）—— 模板由各批自己建，本 helper 只管分档。
func hxFragment(c *gin.Context, name string, data gin.H) bool {
	if !isHXRequest(c) {
		return false
	}
	c.HTML(http.StatusOK, name, data)
	return true
}

// formEchoMemory 解析提交表单时的内存上限（与 gin 的 MaxMultipartMemory 默认值一致）。
const formEchoMemory = 32 << 20

// formEcho 这次请求的表单快照（失败片段回填用）。
//
// 值只来自**这次提交**，不回查数据库：回填要的是「用户刚打的字」，而不是「库里的旧值」
// （新建 / 行内编辑路径上后者会把人刚改的内容顶掉，比不回填更糟）。
type formEcho struct {
	// values 归一化后的提交值：去首尾空白、丢空项（与 shell.FieldValue 同口径）。
	// 多值字段保留**提交顺序** —— 顺序在本域有语义（多仓勾选按表内顺序取认领仓），
	// 排序或去重都会改掉它。
	values url.Values
}

// formEchoFrom 取这次请求的表单快照。
func formEchoFrom(c *gin.Context) formEcho {
	vals := url.Values{}
	if c == nil || c.Request == nil {
		return formEcho{values: vals}
	}
	// 走标准库的解析入口（gin 的 PostForm 也走这里）：urlencoded 与 multipart 两条路径
	// 都会把字段填进 req.PostForm。解析错误一律忽略 —— 解析失败就当「没提交」，
	// 回填退化成空表单；提交本身合法与否由各 handler 的业务校验回答，不在这里变成 500。
	_ = c.Request.ParseMultipartForm(formEchoMemory)
	for key, list := range c.Request.PostForm {
		for _, raw := range list {
			if v := strings.TrimSpace(raw); v != "" {
				vals.Add(key, v)
			}
		}
	}
	return formEcho{values: vals}
}

// value 取单值字段的回填值（缺失返回空串）：文本 / 下拉 / 隐藏域用。
func (e formEcho) value(name string) string {
	if list := e.values[name]; len(list) > 0 {
		return list[0]
	}
	return ""
}

// list 取多值字段的完整回填值（复选框组 / 同名多次提交），保留提交顺序。
func (e formEcho) list(name string) []string {
	return e.values[name]
}

// checked 该字段这次是否被提交过（复选框的回填判据）。
//
// 判据是「字段在提交里存在」：复选框只有被勾选才会提交 —— 所以勾选态用 checked，
// 文本值用 value，两者不要混用（文本框的值可能是空串，那不代表「没填过」）。
func (e formEcho) checked(name string) bool {
	return len(e.values[name]) > 0
}

// formEchoData 把回填值摊成片段 data 可直接并入的三个键。
//
//	"FormEcho"        gin.H{字段名: string}   文本 / 下拉 / 隐藏域 → value="{{ .FormEcho.code }}"
//	"FormEchoChecked" gin.H{字段名: bool}     复选框 → {{if .FormEchoChecked.isDefault}}checked{{end}}
//	"FormEchoMulti"   gin.H{字段名: []string} 多值字段的完整提交值（保序）
//
// **键名带 FormEcho 前缀，不用裸 Form**：回填片段与页面共用同一份渲染 data，而页面
// 自己的键空间里可能早已有 `Form`（商品列表页的 `Form` 是批量改价的 pricingForm **结构体**）。
// 撞名时片段里的 `{{.Form.code}}` 会命中结构体，Jet 报 `can't use code as field name in
// struct type` 并从那一行截断整页（HTTP 仍 200），且只在带那个键的渲染路径上炸。
//
// **字段名要逐个列出**：Jet 读 map 里缺失的键会抛运行时错误、整个片段渲染失败 ——
// 渲染器先渲到 buffer，失败走 `http.Error(500, …)`（buffer 里的半截内容被丢弃），
// 而 htmx 默认把 5xx 判成 `swap:false`：用户点了保存**什么都看不到**。
// 所以列出的字段都被补成零值。模板里访问**未列出**的字段仍要用 isset 包裹
// （见 internal/templates/CLAUDE.md 的「可选数据键必须用 isset 判断」）。
func formEchoData(c *gin.Context, fields ...string) gin.H {
	echo := formEchoFrom(c)
	form := make(gin.H, len(fields))
	checked := make(gin.H, len(fields))
	multi := make(gin.H, len(fields))
	for _, name := range fields {
		form[name] = echo.value(name)
		checked[name] = echo.checked(name)
		multi[name] = echo.list(name)
	}
	return gin.H{"FormEcho": form, "FormEchoChecked": checked, "FormEchoMulti": multi}
}

// purchaseCreateFormFields 是单值字段的回填契约；三列同名采购行保留位置单独处理。
var purchaseCreateFormFields = []string{"projectId", "code", "sourceId", "warehouseId", "remark"}

// purchaseCreateFail 在抽屉内回显原始输入，采购行的空位也不压缩。
func (h *inventoryPurchasePageHandle) purchaseCreateFail(c *gin.Context, projectID string, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryPurchaseBack(c), inventoryPurchaseBackText(c))
		return
	}
	// htmx 分支原样保留：失败在抽屉里重渲片段（错误槽 + 回填后的表单），不丢用户输入。
	// 直接读取 PostForm：通用 formEcho 会剔除空项、修剪首尾空白，导致空行错位。
	_ = c.Request.ParseMultipartForm(formEchoMemory)
	values := c.Request.PostForm
	fields := make(gin.H, len(purchaseCreateFormFields))
	for _, key := range purchaseCreateFormFields {
		fields[key] = values.Get(key)
	}
	rows := make([]gin.H, inventoryPurchaseDraftLines)
	for i := range rows {
		rows[i] = gin.H{
			"Index":     i,
			"SKU":       formArrayAtRaw(values["lineSku"], i),
			"Quantity":  formArrayAtRaw(values["lineQuantity"], i),
			"UnitPrice": formArrayAtRaw(values["lineUnitPrice"], i),
		}
	}
	// 候选项由当前工程重取，表单值只取此次提交。
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	data := gin.H{
		"FormEcho": fields, "SelectedProject": projectID, "DraftLines": rows,
		"Sources":        sourceOptions(ctx, h.inventory, projectID, "", tr),
		"Warehouses":     h.purchaseWarehouseOptions(ctx, projectID, tr),
		"VariantOptions": h.purchaseVariantOptions(ctx, projectID, tr),
		"SubmitErr":      inventoryErrText(c, err),
		"ListQuery":      inventoryQueryFromRequest(c, inventoryPurchaseBackKeys...),
	}
	c.HTML(http.StatusOK, "admin/inventory/inventory_purchase_create_form.html", shell.Prepare(c, data))
}

func formArrayAtRaw(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}

// inventory_purchase_page_form.go - 采购入库表单解析（收货明细行、草稿行与数值解析）。

// purchaseLineSkuField 采购行 SKU 选择器的表单字段名（模板与解析共用同一个字面量）。
//
// 它承载的是三段拼接的引用 —— "<变体ID>|<商品ID>|<仓库侧裸码>"，不是裸的 SKU 编码：
// 本页不引入自定义 JS（多端契约要求交互全走原生控件），一个 <select> 只能提交一个值，
// 而一行的变体 / 商品 / 仓库侧 SKU 必须**同源**（都取自候选列表的同一行）。
// 字段名与值形状两边（模板 + 本文件）必须一起改 —— 历史上正是「handler 读 lineSKUCode、
// 模板却根本没有这个字段」让空串静默通过了校验，见 parsePurchaseSkuRef 的注释。
const purchaseLineSkuField = "lineSku"

// purchaseLinesForm 解析新建表单里的固定几行（空行跳过）。
//
// 驱动数组是 purchaseLineSkuField（未选 SKU 的行整行跳过，数量 / 单价各按同一序号取值）——
// 与改造前用 lineVariantId 驱动是同一个形状，只是那个字段名当时在模板里并不存在。
// SKUCode 为空**不在这里兜底**：归一旦校验在 service（normalizeStockSKU），
// 这条路径只把表单原样翻译成入参（含空值），让唯一的规则在唯一的入口上生效。
func purchaseLinesForm(c *gin.Context) (lines []inventorydto.PurchaseLineReq) {
	refs := c.PostFormArray(purchaseLineSkuField)
	quantities := c.PostFormArray("lineQuantity")
	prices := c.PostFormArray("lineUnitPrice")
	lines = make([]inventorydto.PurchaseLineReq, 0, len(refs))
	for i, raw := range refs {
		variantID, productID, skuCode := parsePurchaseSkuRef(raw)
		if variantID == "" {
			continue
		}
		line := inventorydto.PurchaseLineReq{
			VariantID: variantID,
			ProductID: productID,
			SKUCode:   skuCode,
			Quantity:  parseIntOr(formArrayAt(quantities, i), 0),
			Sort:      i,
		}
		if price, ok := parseFloatOK(formArrayAt(prices, i)); ok {
			line.UnitPrice = price
		}
		lines = append(lines, line)
	}
	return lines
}

// parsePurchaseSkuRef 拆开采购行 SKU 选择器的值 "<变体ID>|<商品ID>|<仓库侧裸码>"。
//
// 按 SplitN(…, 3) 拆：前两段是 uuid（不含 '|'），第三段（SKU 编码）原样收下 ——
// 运营自定义的编码里若真的出现 '|'，也只会在最后一段里，不会被误切。
// 段数不足时后面的段为空串（模板不会这么渲染，但**不能因此 panic 或静默错位**：
// 空值会一路走到 service 的归一校验并被明确拒绝）。
func parsePurchaseSkuRef(raw string) (variantID, productID, skuCode string) {
	parts := strings.SplitN(strings.TrimSpace(raw), "|", 3)
	variantID = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		productID = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		skuCode = strings.TrimSpace(parts[2])
	}
	return variantID, productID, skuCode
}

// formArrayAt 取数组第 i 项（越界返回空串，表单行数与数组长度不一致时不 panic）。
func formArrayAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[index])
}

// parseFloatOK 解析可空小数（空串 / 非法返回 ok=false，交给服务端报参数错误）。
func parseFloatOK(raw string) (value float64, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// purchaseDraftLines 新建窗体里的空行（模板 range 用）。
func purchaseDraftLines() []gin.H {
	out := make([]gin.H, 0, inventoryPurchaseDraftLines)
	for i := 0; i < inventoryPurchaseDraftLines; i++ {
		out = append(out, gin.H{"Index": i})
	}
	return out
}

const (
	// inventoryPurchasePageSize 采购单页一次列出的单据数（每页条数；完整清单靠分页翻）。
	//
	// 原先它同时充当「硬编码上限」—— 第 101 张采购单静默消失且页面上没有任何提示。
	// 现在它是**每页条数**（可用 ?limit= 调小，上限见 inventoryPurchaseMaxPageSize），
	// 页面下方给分页条。
	inventoryPurchasePageSize = 100
	// inventoryPurchaseMaxPageSize 采购页每页条数的上限（?limit= 的封顶值）。
	//
	// 与 service 的 maxPurchasePageSize 同口径：超过它 service 会自己截到 200，
	// 页面若允许更大的值，URL 上写着 500 而实际只回 200 —— 「翻页少一截」这类缺陷
	// 页面不报错，只是数据对不上。
	inventoryPurchaseMaxPageSize = 200
	// inventoryPurchasesPath 采购入库页路径（回跳地址的唯一定义处，与库存页的
	// inventoryPagePath 同形：handler 不手写字符串，抄错一次就是回跳到 404）。
	inventoryPurchasesPath = "/admin/inventory/purchases"
	// inventoryPurchaseHistorySize 进货历史一次列出的条数（页面当前不再渲染这张表，
	// 取数口保留，见 purchaseHistoryRows）。
	inventoryPurchaseHistorySize = 50
	// inventoryPurchaseDraftLines 新建采购单表单的行数（原生表单：固定几行）。
	inventoryPurchaseDraftLines = 3
	// inventoryPurchaseStockProbeSize 取「该商品各仓库存行」的上限（扒仓库侧裸码用）。
	// 与库存列表的 maxPageSize 同量级：它的用途只是给下拉的每一项配一个裸码，
	// 一个商品的变体数不会接近这个量级。
	inventoryPurchaseStockProbeSize = 200
)

// inventoryPurchasePageHandle 采购入库页处理器。
type inventoryPurchasePageHandle struct {
	inventory inventorycontract.InventoryService
	projects  projectcontract.ProjectService
	// products 只用于 SKU 下拉（商品 → 变体），不参与任何库存 / 采购判断。
	products productcontract.ProductService
}

// NewInventoryPurchasePageHandle 构造。
//
// products 只用于 SKU 下拉：装配期传入 nil，装配收尾由 SetProductCatalog 补注
// （商品模块与本模块互为依赖，装配顺序上无法在此时拿到商品契约）。
func NewInventoryPurchasePageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService, products productcontract.ProductService) *inventoryPurchasePageHandle {
	return &inventoryPurchasePageHandle{inventory: inventory, projects: projects, products: products}
}

// setProductCatalog 装配期补注商品契约（见 inventory_router.go 的 SetProductCatalog）。
func (h *inventoryPurchasePageHandle) setProductCatalog(products productcontract.ProductService) {
	h.products = products
}

// inventoryPurchasesPageData 采购入库页的模板数据（正常渲染与「装载失败降级渲染」共用一份拼装）。
//
// 与货源页同源的理由：模板的顶层 let（selectedProject / orders / filter* / receiptRequestID）
// 与页面直接读取的键都必须存在，缺一个就是整页中断（HTTP 200 + 后面整块 HTML 消失）。
type inventoryPurchasesPageData struct {
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Sources         []gin.H
	InternalSources []gin.H
	Warehouses      []gin.H
	VariantOptions  []gin.H
	Orders          []gin.H
	FilterStatus    string
	FilterSource    string
	FilterKeyword   string
	// Err 页面提示条：只剩一处来源 —— 本页取数失败（工程列表 / 采购单列表）。
	// 写动作的结论不再回带（走 shell.RenderJump，见 inventory_jump.go）。
	Err string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）：与 order 域订单页 / 退货页同名，
	// 模板据此把「真的没有货源 / 没有采购单」与「这一次没读出来」分开 —— 前者引导去建一个，
	// 后者只能说明稍后重试，把人引向新建抽屉是错的。
	LoadFailed bool
	// Pagination 分页条数据（nil = 单页 / 装载失败，模板不渲染分页条）。
	//
	// 放在结构体里而不是在渲染处临时拼：正常与降级两条路共用一份 templateMap，
	// 降级分支才不会「忘记」给某个键（Jet 缺 key 是整页中断）。
	Pagination *shell.PaginationData
}

// templateMap 转 Jet 模板键（一次性幂等键在这里生成：每次渲染一个新的，双击提交即被挡住）。
//
// c 只用于取词：状态下拉文案按请求语言渲染。
func (d *inventoryPurchasesPageData) templateMap(c *gin.Context) gin.H {
	m := gin.H{
		"title":           inventoryenums.MsgInventoryPurchasesTitle,
		"menu":            "inventory-purchases",
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"Sources":         d.Sources,
		"InternalSources": d.InternalSources,
		"Warehouses":      d.Warehouses,
		"VariantOptions":  d.VariantOptions,
		"DraftLines":      purchaseDraftLines(),
		"Orders":          d.Orders,
		"StatusOptions":   purchaseStatusOptions(shell.TranslateFor(c)),
		"FilterStatus":    d.FilterStatus,
		"FilterSource":    d.FilterSource,
		"FilterKeyword":   d.FilterKeyword,
		// 一次性幂等键：每个写表单一次渲染一个，双击提交只会产生一张入库单 / 一次入库。
		// 两个入口**各用各的键**：幂等键在入库单上是全局唯一的，共用一个键会让
		// 「先收采购货，再做生产入库」的第二跳被判成同一张单的重放而静默丢弃。
		"ReceiptRequestID":    uuid.NewString(),
		"ProductionRequestID": uuid.NewString(),
		"Err":                 d.Err,
		"LoadFailed":          d.LoadFailed,
		// 写动作表单 action 上带的筛选上下文（见 inventory_jump.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventoryPurchaseBackKeys...),
	}
	// 分页条键（PaginationInfo / PaginationLinks）：nil 时给空 map，模板的
	// {{if .["PaginationLinks"]}} 自然跳过 —— 单页与装载失败两条路都不渲染分页条。
	for k, v := range d.Pagination.TemplateKeys() {
		m[k] = v
	}
	return m
}

// renderPurchasesPage 采购页的唯一渲染出口（正常 / 装载失败两条路共用）。
func (h *inventoryPurchasePageHandle) renderPurchasesPage(c *gin.Context, d *inventoryPurchasesPageData) {
	c.HTML(http.StatusOK, "admin/inventory/inventory_purchases.html", shell.Prepare(c, d.templateMap(c)))
}

// InventoryPurchasesPage 采购入库页：新建采购单 + 采购单列表（含逐行收货）+ 生产入库 + 进货历史。
func (h *inventoryPurchasePageHandle) InventoryPurchasesPage(c *gin.Context) {
	ctx := c.Request.Context()

	// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 inventory_jump.go），
	// 所以提示条只剩一处来源：本页取数失败（工程列表 / 采购单列表）。
	pageErr := ""

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据与货源页 / order 域订单页 / project 域主题页一致）：
	// 空数据 + 归口提示 + HTTP 200，页头 / 筛选栏 / 侧栏全部保留。
	//
	// 原先这里是 `c.String(500, shell.MsgInternalError)`：页面上就是 `MsgInternalError`
	// 这串英文 —— 归口 key 未经翻译直出，症状比脱壳更隐蔽（看起来像后台坏了）。
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		pageErr = inventoryErrText(c, loadErr)
	}

	// 装载失败时不再去读列表与各下拉候选：工程上下文没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查采购单，查出来的是哪个工程的单据都说不清。
	selected := ""
	if !loadFailed {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	filterStatus := strings.TrimSpace(c.Query("status"))
	filterSource := strings.TrimSpace(c.Query("sourceId"))
	filterKeyword := strings.TrimSpace(c.Query("keyword"))

	sources := []gin.H{}
	internalSources := []gin.H{}
	warehouses := []gin.H{}
	variants := []gin.H{}
	orders := []gin.H{}
	var total int64
	// 分页（审计 D3）：?page= / ?limit=，缺省每页 100 张单据。原先这一页写死 100 且
	// 没有分页条 —— 第 101 张采购单静默消失，页面上没有任何提示。
	page, limit := inventoryPageParams(c, inventoryPurchasePageSize, inventoryPurchaseMaxPageSize)
	if !loadFailed {
		// 本页承载两个入库入口：采购单 → 逐行收货，以及自家工厂**生产入库**
		// （无采购单、成本价手工填，验收 5）。进货历史不在这里 —— 它就是「按 SKU 看库存流水」，
		// 已并入库存管理页的流水筛选（同一查询对象的另一个视角）。
		tr := shell.TranslateFor(c)
		sources = sourceOptions(ctx, h.inventory, selected, "", tr)
		// 生产入库的来源只列**内部货源**（自家工厂 / 集团内关联公司）：外部供应商走采购单，
		// 没有「无采购单的生产入库」这一说（服务端同样拒绝，见 ErrProductionSourceNotInternal）。
		internalSources = sourceOptions(ctx, h.inventory, selected, inventoryenums.SourceTypeInternal, tr)
		warehouses = h.purchaseWarehouseOptions(ctx, selected, tr)
		variants = h.purchaseVariantOptions(ctx, selected, tr)
		orders, total, page = h.purchaseOrderRows(c, selected, filterStatus, filterSource, filterKeyword,
			page, limit, &pageErr)
	}

	// 分页条（nil = 单页 / 空数据 / 装载失败，模板不渲染）：基址带当前全部筛选维度，
	// 翻页不丢条件。total 来自契约的 CountPurchaseOrders（与 ListPurchaseOrders
	// 同一份过滤条件），因此给的是页码窗口与「共 N 条」。
	// 装载失败时不构造分页条：工程上下文没定下来，URL 上的 page 不是有效页码。
	var pagination *shell.PaginationData
	if !loadFailed {
		pagination = shell.BuildPagination(total, page, limit, shell.FilterBaseURL(
			inventoryPurchasesPath, map[string]string{
				"project":  selected,
				"status":   filterStatus,
				"sourceId": filterSource,
				"keyword":  filterKeyword,
			}), shell.TranslateFor(c))
	}

	h.renderPurchasesPage(c, &inventoryPurchasesPageData{
		Projects:        projects,
		SelectedProject: selected,
		Sources:         sources,
		InternalSources: internalSources,
		Warehouses:      warehouses,
		VariantOptions:  variants,
		Orders:          orders,
		FilterStatus:    filterStatus,
		FilterSource:    filterSource,
		FilterKeyword:   filterKeyword,
		Err:             pageErr,
		LoadFailed:      loadFailed,
		Pagination:      pagination,
	})
}

// InventoryPurchaseCreate 新建采购单（单头 + 结构化行）。
func (h *inventoryPurchasePageHandle) InventoryPurchaseCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.CreatePurchaseOrderReq{
		ProjectID:   projectID,
		Code:        strings.TrimSpace(c.PostForm("code")),
		SourceID:    strings.TrimSpace(c.PostForm("sourceId")),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		Remark:      strings.TrimSpace(c.PostForm("remark")),
		OperatorID:  builtin.GetUsername(c),
		Lines:       purchaseLinesForm(c),
	}
	if _, err := h.inventory.CreatePurchaseOrder(c.Request.Context(), req); err != nil {
		h.purchaseCreateFail(c, projectID, err)
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryPurchaseBack(c), inventoryPurchaseBackText(c))
}

// InventoryPurchaseReceipt 登记某一行（或多行）采购收货。
//
// 表单是「按行提交」：每行一个独立表单，因此这里只需处理一行；接口侧的批量收货运维用。
func (h *inventoryPurchasePageHandle) InventoryPurchaseReceipt(c *gin.Context) {
	projectID := c.PostForm("projectId")
	quantity := parseIntOr(c.PostForm("quantity"), 0)
	req := &inventorydto.RegisterReceiptReq{
		ProjectID:  projectID,
		OrderID:    c.PostForm("orderId"),
		RequestID:  strings.TrimSpace(c.PostForm("requestId")),
		Remark:     strings.TrimSpace(c.PostForm("remark")),
		OperatorID: builtin.GetUsername(c),
		Lines: []inventorydto.ReceiptLineReq{{
			LineID:   strings.TrimSpace(c.PostForm("lineId")),
			Quantity: quantity,
		}},
	}
	if price, ok := parseFloatOK(c.PostForm("unitPrice")); ok {
		req.Lines[0].UnitPrice = &price
	}
	if _, err := h.inventory.RegisterReceipt(c.Request.Context(), req); err != nil {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryPurchaseBack(c), inventoryPurchaseBackText(c))
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryPurchaseBack(c), inventoryPurchaseBackText(c))
}

// InventoryPurchaseProduction 自家工厂生产入库（无采购单，成本价手工填写）。
//
// SKU 走本页统一的「三段值」选择器（purchaseLineSkuField）：一个原生下拉只能提交一个值，
// 而一行的变体 / 商品 / 仓库侧裸码必须**同源**（都取自候选列表的同一行），所以用
// "<变体ID>|<商品ID>|仓库侧裸码>" 拼成一个值 —— 与新建采购单的采购行逐字同一口径，
// 解析也用同一个 parsePurchaseSkuRef。字段名两边（模板 + 本文件）必须一起改。
func (h *inventoryPurchasePageHandle) InventoryPurchaseProduction(c *gin.Context) {
	projectID := c.PostForm("projectId")
	variantID, productID, skuCode := parsePurchaseSkuRef(c.PostForm(purchaseLineSkuField))
	req := &inventorydto.ProductionInboundReq{
		ProjectID:   projectID,
		SourceID:    strings.TrimSpace(c.PostForm("sourceId")),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		VariantID:   variantID,
		ProductID:   productID,
		SKUCode:     skuCode,
		Quantity:    parseIntOr(c.PostForm("quantity"), 0),
		RequestID:   strings.TrimSpace(c.PostForm("requestId")),
		Remark:      strings.TrimSpace(c.PostForm("remark")),
		OperatorID:  builtin.GetUsername(c),
	}
	if cost, ok := parseFloatOK(c.PostForm("unitCost")); ok {
		req.UnitCost = &cost
	}
	if _, err := h.inventory.RegisterProductionInbound(c.Request.Context(), req); err != nil {
		inventoryJump(c, false, inventoryErrText(c, err), inventoryPurchaseBack(c), inventoryPurchaseBackText(c))
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventoryPurchaseBack(c), inventoryPurchaseBackText(c))
}

// —— 页面取数（视图组装：模板不做逻辑）——

// inventory_purchase_page_view.go - 采购入库页的视图构造（采购单/历史行、货源与仓库/变体下拉、状态文案）。

// purchaseOrderRows 采购单列表 + 逐行视图（含「还能收多少」与一次性幂等键）。
//
// 取数失败回显走 inventoryErrText（业务 key → 当前语言文案，其余 → 归口文案 + 结构化日志），
// 与写路径同一口径：列表页的失败提示不允许比写路径多泄漏一个字符。
//
// total 是该过滤条件下的**真实总张数**（契约的 CountPurchaseOrders，与 ListPurchaseOrders
// 同一份过滤条件）；curPage 是**收敛后**的页码（page 越界时回落到末页）。out 只装本页数据。
func (h *inventoryPurchasePageHandle) purchaseOrderRows(c *gin.Context, projectID, status, sourceID, keyword string,
	page, limit int, pageErr *string) (out []gin.H, total int64, curPage int) {
	ctx := c.Request.Context()
	out = []gin.H{}
	if projectID == "" {
		return out, 0, 1
	}
	// 过滤条件只构造一次：列表与计数各自复制、只加各自的 Page/Size ——
	// 两处各写一份时，日后新增一个筛选维度只改到列表那一侧，「共 N 条」就会与实际条数矛盾。
	filterReq := &inventorydto.ListPurchaseOrderReq{
		ProjectID: projectID, Status: status, SourceID: sourceID, Keyword: keyword,
	}
	// 先计数、收敛页码，再取当页数据（顺序不能反，见 clampInventoryPage）：
	// 反过来时 page 越界会让列表返回空页，而分页条按收敛后的页码渲染。
	n, cerr := h.inventory.CountPurchaseOrders(ctx, filterReq)
	if cerr != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, cerr)
		}
		return out, 0, page
	}
	total = n
	curPage = clampInventoryPage(page, limit, total)
	listReq := *filterReq
	listReq.Page, listReq.Size = curPage, limit
	orders, err := h.inventory.ListPurchaseOrders(ctx, &listReq)
	if err != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, err)
		}
		return out, total, curPage
	}
	tr := shell.TranslateFor(c)
	for _, o := range orders {
		lines := make([]gin.H, 0, len(o.Lines))
		for _, l := range o.Lines {
			lines = append(lines, gin.H{
				"ID": l.ID, "SKUCode": l.SKUCode, "VariantID": l.VariantID,
				"Quantity": l.Quantity, "ReceivedQuantity": l.ReceivedQuantity,
				"OutstandingQuantity": l.OutstandingQuantity,
				"UnitPrice":           strconv.FormatFloat(l.UnitPrice, 'f', 2, 64),
				"Remark":              l.Remark,
				// 还能收的行才给提交按钮：收满的行不再受理（服务端同样拒绝）。
				"Open": l.OutstandingQuantity > 0,
			})
		}
		out = append(out, gin.H{
			"ID": o.ID, "Code": o.Code,
			"SourceName": o.SourceName, "SourceType": o.SourceType,
			"SourceTypeLabel": sourceTypeLabel(tr, o.SourceType),
			"WarehouseName":   o.WarehouseName,
			"Status":          o.Status, "StatusLabel": purchaseStatusLabel(tr, o.Status),
			"OrderedAt": o.OrderedAt, "ExpectedAt": o.ExpectedAt,
			"Remark": o.Remark, "OperatorID": o.OperatorID,
			"TotalQuantity": o.TotalQuantity, "ReceivedQuantity": o.ReceivedQuantity,
			"Lines": lines,
			// 每张单一个一次性幂等键（表单渲染时生成，重复提交命中同一张入库单）。
			"RequestID": uuid.NewString(),
		})
	}
	return out, total, curPage
}

// purchaseHistoryRows 按 SKU（可空 = 全部）取进货历史。
//
// 当前页面不再渲染这张表（「按 SKU 看过往入库」已并入库存管理页的流水筛选），
// 但取数口保留：接口侧与将来的页面入口共用同一投影。错误回显同样走 inventoryErrText——
// 留着 err.Error() 就等于在同一个文件里留了一条尚未被页面用到的泄漏路径。
func (h *inventoryPurchasePageHandle) purchaseHistoryRows(c *gin.Context, projectID, sku string, pageErr *string) (out []gin.H) {
	ctx := c.Request.Context()
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	rows, err := h.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: projectID, SKUCode: sku, Size: inventoryPurchaseHistorySize,
	})
	if err != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, err)
		}
		return out
	}
	tr := shell.TranslateFor(c)
	for _, r := range rows {
		out = append(out, gin.H{
			"ReceiptCode": r.ReceiptCode, "Kind": r.Kind, "KindLabel": receiptKindLabel(tr, r.Kind),
			"OrderCode": r.OrderCode, "SourceName": r.SourceName,
			"SourceTypeLabel": sourceTypeLabel(tr, r.SourceType),
			"WarehouseName":   r.WarehouseName, "SKUCode": r.SKUCode,
			"Quantity": r.Quantity, "UnitPrice": strconv.FormatFloat(r.UnitPrice, 'f', 2, 64),
			"CostUpdated": r.CostUpdated, "Remark": r.Remark,
			"OperatorID": r.OperatorID, "ReceivedAt": r.ReceivedAt,
		})
	}
	return out
}

// sourceOptions 货源下拉（sourceType 非空时只看该类型：内部类型 = 自家工厂 / 集团内关联公司）。
//
// 包级函数而非某个 handler 的方法：库存管理与采购入库两个页面都要用 ——
// 「生产入库」已从采购页归位到库存管理（它与采购单无关，本质是「手动改库存 + 写成本价」）。
func sourceOptions(ctx context.Context, svc inventorycontract.InventoryService, projectID, sourceType string,
	tr func(key, fallback string) string) (out []gin.H) {
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	rows, err := svc.ListSources(ctx, &inventorydto.ListSourceReq{
		ProjectID: projectID, Type: sourceType,
	})
	if err != nil {
		return out
	}
	for _, s := range rows {
		out = append(out, gin.H{
			"ID": s.ID, "Code": s.Code, "Name": s.Name, "Type": s.Type,
			"Label": sourceOptionLabel(tr, s),
		})
	}
	return out
}

// sourceOptionLabel 货源下拉项的文案：「名称（编码）· 类型」。类型走取词（见 sourceTypeLabel）。
func sourceOptionLabel(tr func(key, fallback string) string, s *inventorydto.SourceResp) string {
	return s.Name + "（" + s.Code + "）· " + sourceTypeLabel(tr, s.Type)
}

// purchaseWarehouseOptions 收货仓下拉（默认仓标出来；未选即兜底默认仓）。
func (h *inventoryPurchasePageHandle) purchaseWarehouseOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []gin.H) {
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	rows, err := h.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return out
	}
	for _, w := range rows {
		label := w.Name + "（" + w.Code + "）"
		if w.IsDefault {
			label += " " + tr(inventoryenums.InventoryChangeWarehouseDefaultSuffix, "· 默认仓")
		}
		out = append(out, gin.H{"ID": w.ID, "Label": label, "IsDefault": w.IsDefault})
	}
	return out
}

// purchaseVariantOptions 变体下拉（带商品名 / SKU / 当前成本价，选行时一眼可见成本口径）。
//
// 每一项额外带 **BareSKU（仓库侧裸码）**：采购行的 sku_code 是仓库侧快照，必须按裸码落库
// （仓库里的 SKU 永远不带仓码前缀，见 docs/14 §1.1 与迁移 262）。裸码取自本模块自己的
// 库存真源（inventory_stocks.sku_code）—— 它就是「这条货在仓库里叫什么」的权威答案，
// 商品侧的 v.SKUCode 只是它加了认领仓前缀的投影。没有库存行的变体退回商品侧编码，
// 由服务端在入库入口按目标仓短码归一（inventory_stock.go）。
func (h *inventoryPurchasePageHandle) purchaseVariantOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []gin.H) {
	out = []gin.H{}
	// 商品契约未注入（装配漏接）时下拉为空，页面照常渲染 —— 不因一处装配缺失 500。
	if projectID == "" || h.products == nil {
		return out
	}
	list, err := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Size: 100})
	if err != nil {
		return out
	}
	// 成本后缀是一句带命名占位符的文案：取词后用 {value} 填充（词条被写坏时 FillTranslate
	// 自动回落下面那句中文兜底，页面上不会出现 {value} 这样的字面量）。
	costSuffix := func(cost string) string {
		return i18n.FillTranslate(tr, inventoryenums.InventoryPurchasesOptionCostLabel, "（成本 {value}）",
			map[string]string{"value": cost})
	}
	unsetCost := tr(inventoryenums.InventoryPurchasesOptionCostUnset, "未填")
	for _, p := range list {
		detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
		if derr != nil {
			continue
		}
		bare := h.productBareSKUs(ctx, projectID, p.ID)
		for _, v := range detail.Variants {
			cost := unsetCost
			if v.CostPrice != nil {
				cost = strconv.FormatFloat(*v.CostPrice, 'f', 2, 64)
			}
			sku := bare[v.ID]
			if sku == "" {
				sku = v.SKUCode
			}
			out = append(out, gin.H{
				"VariantID": v.ID, "ProductID": p.ID, "SKUCode": v.SKUCode, "BareSKU": sku,
				"Label": detail.Name + " · " + v.SKUCode + costSuffix(cost),
			})
		}
	}
	return out
}

// productBareSKUs 某商品全部变体的**仓库侧裸码**（变体 id → inventory_stocks.sku_code）。
//
// 一次取回该商品在各仓的库存行（按商品过滤，与页面既有的「逐商品取详情」同量级），
// 变体在哪个仓有行都算 —— 裸码是变体的仓库侧身份，与具体哪个仓无关。
// 读失败不抛给页面（返回空 map，调用方退回商品侧编码）：采购页不该因为一次展示取数
// 失败而 500，真正的守门在入库入口的归一路径上。
func (h *inventoryPurchasePageHandle) productBareSKUs(ctx context.Context, projectID, productID string) map[string]string {
	out := map[string]string{}
	rows, err := h.inventory.ListStocks(ctx, &inventorydto.ListStockReq{
		ProjectID: projectID, ProductID: productID, Size: inventoryPurchaseStockProbeSize,
	})
	if err != nil {
		return out
	}
	for _, r := range rows {
		if r.VariantID != "" && r.SKUCode != "" {
			out[r.VariantID] = r.SKUCode
		}
	}
	return out
}

// —— 表单与文案工具 ——

// purchaseStatusOptions 状态筛选下拉（三种推导值）。
func purchaseStatusOptions(tr func(key, fallback string) string) []gin.H {
	return []gin.H{
		{"Value": inventoryenums.PurchaseStatusPending, "Label": purchaseStatusLabel(tr, inventoryenums.PurchaseStatusPending)},
		{"Value": inventoryenums.PurchaseStatusPartial, "Label": purchaseStatusLabel(tr, inventoryenums.PurchaseStatusPartial)},
		{"Value": inventoryenums.PurchaseStatusReceived, "Label": purchaseStatusLabel(tr, inventoryenums.PurchaseStatusReceived)},
	}
}

// purchaseStatusLabel 推导状态 → 当前语言展示文案。
func purchaseStatusLabel(tr func(key, fallback string) string, status string) string {
	switch status {
	case inventoryenums.PurchaseStatusPending:
		return tr(inventoryenums.InventoryPurchasesStatusPending, "未入库")
	case inventoryenums.PurchaseStatusPartial:
		return tr(inventoryenums.InventoryPurchasesStatusPartial, "部分入库")
	case inventoryenums.PurchaseStatusReceived:
		// 已入库：与同页列头同一个词（英文都是 Received），复用它的词条。
		return tr(inventoryenums.InventoryPurchasesColReceived, "已入库")
	default:
		return status
	}
}

// receiptKindLabel 入库单类型 → 当前语言展示文案。
func receiptKindLabel(tr func(key, fallback string) string, kind string) string {
	switch kind {
	case inventoryenums.ReceiptKindPurchase:
		return tr(inventoryenums.InventoryPurchasesKindPurchase, "采购收货")
	case inventoryenums.ReceiptKindProduction:
		// 生产入库：与采购页的「生产入库」入口同一个词，复用它的词条。
		return tr(inventoryenums.InventoryPurchasesProductionOpen, "生产入库")
	default:
		return kind
	}
}

// sourceStatusFilterAll 状态筛选的「全部（含停用）」取值。
const sourceStatusFilterAll = "all"

// inventorySourcePageSize 货源页一次列出的条数（每页条数；完整清单靠分页翻）。
//
// 原先它同时充当「硬编码上限」—— 第 201 个货源静默消失且页面上没有任何提示。
// 现在它是**每页条数**（可用 ?limit= 调小，上限见 inventorySourceMaxPageSize），
// 页面下方给分页条，第 201 个之后靠翻页看到。
const inventorySourcePageSize = 200

// inventorySourceMaxPageSize 货源页每页条数的上限（?limit= 的封顶值）。
//
// 与 service 的 maxSourcePageSize 同口径：超过它 service 会自己截到 200，
// 页面若允许更大的值，URL 上写着 500 而实际只回 200 —— 「翻页少一截」这类缺陷
// 页面不报错，只是数据对不上，最难查。
const inventorySourceMaxPageSize = 200

// inventorySourcePageHandle 货源管理页处理器。
type inventorySourcePageHandle struct {
	inventory inventorycontract.InventoryService
	projects  projectcontract.ProjectService
}

// NewInventorySourcePageHandle 构造。
func NewInventorySourcePageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService) *inventorySourcePageHandle {
	return &inventorySourcePageHandle{inventory: inventory, projects: projects}
}

// inventorySourcesPageData 货源管理页的模板数据（正常渲染与「装载失败降级渲染」共用一份拼装）。
//
// 单独一个类型的理由与 project 域主题管理页同源：降级渲染若另抄一份 gin.H，两处的键集
// 必然分叉 —— 而 Jet 的可选键缺 key 是**整页中断**（HTTP 200 + 后面整块 HTML 消失），
// 症状比脱壳的纯文本更难看出来。
type inventorySourcesPageData struct {
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Sources         []gin.H
	HasSummary      bool
	Summary         gin.H
	FilterType      string
	FilterRelated   string
	FilterStatus    string
	FilterKeyword   string
	// Err 页面提示条：只剩一处来源 —— 本页取数失败（工程列表 / 货源列表 / 统计）。
	// 写动作的结论不再回带（走 shell.RenderJump，见 inventory_jump.go）。
	Err string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）。
	//
	// 与 order 域订单页 / 退货页同名的判据：模板里没有可靠的办法分辨「Sources 为空」
	// 是「这个工程真的没有货源」还是「这一次没读出来」—— 前者要引导去建一个，
	// 后者只能说明「稍后重试」，把人引向新建抽屉是错的。
	LoadFailed bool
	// Pagination 分页条数据（nil = 单页 / 装载失败，模板不渲染分页条）。
	//
	// 与其它键一样放在结构体里：正常渲染与降级渲染共用一份 templateMap，
	// 降级分支才不会「忘记」给某个键（Jet 缺 key 是整页中断，见 internal/templates/CLAUDE.md）。
	Pagination *shell.PaginationData
}

// templateMap 转 Jet 模板键（页面框架字段以小写 title / menu 取值）。
//
// c 只用于取词：下拉项文案按请求语言渲染（取词一律经 tr(key, 中文兜底)）。
func (d *inventorySourcesPageData) templateMap(c *gin.Context) gin.H {
	tr := shell.TranslateFor(c)
	m := gin.H{
		"title":           inventoryenums.MsgInventorySourcesTitle,
		"menu":            "inventory-sources",
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"Sources":         d.Sources,
		"HasSummary":      d.HasSummary,
		"Summary":         d.Summary,
		"TypeOptions":     sourceTypeOptions(tr),
		"StatusOptions":   sourceStatusOptions(tr),
		"RelatedOptions":  sourceRelatedOptions(tr),
		"FilterType":      d.FilterType,
		"FilterRelated":   d.FilterRelated,
		"FilterStatus":    d.FilterStatus,
		"FilterStatusAll": sourceStatusFilterAll,
		"FilterKeyword":   d.FilterKeyword,
		"Err":             d.Err,
		"LoadFailed":      d.LoadFailed,
		// 写动作表单 action 上带的筛选上下文（见 inventory_jump.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventorySourceBackKeys...),
	}
	// 分页条键（PaginationInfo / PaginationLinks）：nil 时给空 map，模板的
	// {{if .["PaginationLinks"]}} 自然跳过 —— 单页与装载失败两条路都不渲染分页条。
	for k, v := range d.Pagination.TemplateKeys() {
		m[k] = v
	}
	return m
}

// renderSourcesPage 货源页的唯一渲染出口：正常与降级两条路都从这里出，
// 键集只有一处定义（降级分支不必「记得」补齐模板要的每一个键）。
func (h *inventorySourcePageHandle) renderSourcesPage(c *gin.Context, d *inventorySourcesPageData) {
	data := d.templateMap(c)
	data["SourceCreateForm"] = sourceFormData(c, false, nil)
	c.HTML(http.StatusOK, "admin/inventory/inventory_sources.html", shell.Prepare(c, data))
}

// InventorySourcesPage 货源管理页：工程切换 + 关联方统计 + 筛选 + 新建 + 列表（可编辑）。
func (h *inventorySourcePageHandle) InventorySourcesPage(c *gin.Context) {
	ctx := c.Request.Context()

	// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 inventory_jump.go），
	// 所以提示条只剩一处来源：本页取数失败（工程列表 / 货源列表 / 统计）。
	pageErr := ""

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据与 order 域订单页 / project 域主题页一致）：
	// 空列表 + 归口提示 + HTTP 200，页头 / 筛选栏 / 批量条与侧栏全部保留 ——
	// 运营看得出「是这一页没读出来」，而不是对着一块纯文本以为整个后台坏了。
	//
	// 原先这里是 `c.String(500, shell.MsgInternalError)`：响应的是一块**裸归口 key** 的纯文本，
	// 页面上显示的就是 `MsgInternalError` 这串英文（既没翻译、也没页壳）。
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		pageErr = inventoryErrText(c, loadErr)
	}

	// 装载失败时不再去读列表与统计：工程上下文没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查货源，查出来的是哪个工程的货源都说不清。
	selected := ""
	if !loadFailed {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	filterType := strings.TrimSpace(c.Query("type"))
	filterRelated := strings.TrimSpace(c.Query("relatedParty"))
	filterStatus := strings.TrimSpace(c.Query("status"))
	filterKeyword := strings.TrimSpace(c.Query("keyword"))

	// 状态筛选三态：""（默认只列启用中）/ active / disabled / all（连停用的一起列）。
	// 用 all 而不是让空值同时表示「全部」，否则「默认视图」与「全量视图」无法区分。
	includeDisabled := filterStatus == sourceStatusFilterAll
	if includeDisabled {
		filterStatus = ""
	}

	sources := []gin.H{}
	// 与 sourceSummary 的失败分支同形：模板只在 HasSummary 为真时读 Summary，
	// 但键本身必须在（组内键缺失同样会中断渲染）。
	summary := gin.H{"Groups": []gin.H{}}
	hasSummary := false
	var total int64
	// 分页（审计 D3）：?page= / ?limit=，缺省每页 200 条。原先这一页写死 200 且没有分页条，
	// 第 201 个货源**静默消失**、页面上没有任何提示。
	page, limit := inventoryPageParams(c, inventorySourcePageSize, inventorySourceMaxPageSize)
	if !loadFailed {
		// 过滤条件只构造一次：列表与计数各自复制、只加各自的 Page/Size。
		// 两处各写一份时，最容易漏的是 IncludeDisabled 决定的那一档默认状态 ——
		// 计数把停用的也算进去，分页条就会凭空多出一页空列表。
		filterReq := &inventorydto.ListSourceReq{
			ProjectID: selected, Type: filterType, RelatedParty: filterRelated,
			Status: filterStatus, Keyword: filterKeyword,
			IncludeDisabled: includeDisabled,
		}
		// 先计数、收敛页码，再取当页数据（顺序不能反，见 clampInventoryPage）。
		if n, cerr := h.inventory.CountSources(ctx, filterReq); cerr != nil {
			// 筛选参数不合法等：把业务错误回显到页面，不把内部细节直出。
			// 统一走库存域的「错误文案三件套」：命中白名单 → 原样业务文案；否则记结构化日志 + 归口文案
			//（模板只渲染这一份成品文案，不再对 .Err 二次取词）。
			if pageErr == "" {
				pageErr = inventoryErrText(c, cerr)
			}
		} else {
			total = n
			page = clampInventoryPage(page, limit, total)
			listReq := *filterReq
			listReq.Page, listReq.Size = page, limit
			list, lerr := h.inventory.ListSources(ctx, &listReq)
			if lerr != nil {
				if pageErr == "" {
					pageErr = inventoryErrText(c, lerr)
				}
			} else {
				tr := shell.TranslateFor(c)
				for _, s := range list {
					sources = append(sources, sourceRow(tr, s))
				}
			}
		}
		summary, hasSummary = h.sourceSummary(c, ctx, selected, &pageErr)
	}

	// 分页条（nil = 单页 / 空数据 / 装载失败，模板不渲染）：基址带当前全部筛选维度，
	// 翻页不丢条件。
	// status 用**原始取值**：includeDisabled 时 FilterStatus 已被归一成空串（模板的
	// 「全部（含停用）」是另一个键 FilterStatusAll），拿归一后的值拼链接会让翻页时
	// 「连停用的一起列」被悄悄丢掉 —— 表现为翻页后记录变少，看着像数据丢了。
	var pagination *shell.PaginationData
	if !loadFailed {
		statusForLink := filterStatus
		if includeDisabled {
			statusForLink = sourceStatusFilterAll
		}
		pagination = shell.BuildPagination(total, page, limit, shell.FilterBaseURL(
			"/admin/inventory/sources", map[string]string{
				"project":      selected,
				"type":         filterType,
				"relatedParty": filterRelated,
				"status":       statusForLink,
				"keyword":      filterKeyword,
			}), shell.TranslateFor(c))
	}

	h.renderSourcesPage(c, &inventorySourcesPageData{
		Projects:        projects,
		SelectedProject: selected,
		Sources:         sources,
		HasSummary:      hasSummary,
		Summary:         summary,
		FilterType:      filterType,
		FilterRelated:   filterRelated,
		FilterStatus:    filterStatus,
		FilterKeyword:   filterKeyword,
		Err:             pageErr,
		LoadFailed:      loadFailed,
		Pagination:      pagination,
	})
}

// InventorySourceCreate 新建货源（内部类型自动成为关联方）。
func (h *inventorySourcePageHandle) InventorySourceCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.CreateSourceReq{
		ProjectID:    projectID,
		Code:         strings.TrimSpace(c.PostForm("code")),
		Name:         strings.TrimSpace(c.PostForm("name")),
		Type:         strings.TrimSpace(c.PostForm("type")),
		Status:       strings.TrimSpace(c.PostForm("status")),
		RelatedParty: sourceRelatedForm(c.PostForm("relatedParty")),
		Config:       sourceConfigForm(c.PostForm("config")),
		Sort:         parseIntOr(c.PostForm("sort"), 0),
	}
	price, perr := sourceSettleForm(c.PostForm("settlePrice"))
	if perr != nil {
		h.sourceFormFail(c, false, perr)
		return
	}
	req.SettlePrice = price
	if _, err := h.inventory.CreateSource(c.Request.Context(), req); err != nil {
		h.sourceFormFail(c, false, err)
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventorySourceBack(c), inventorySourceBackText(c))
}

// InventorySourceUpdate 修改货源（表单即最终状态：结算价留空 = 清空）。
func (h *inventorySourcePageHandle) InventorySourceUpdate(c *gin.Context) {
	code := strings.TrimSpace(c.PostForm("code"))
	name := strings.TrimSpace(c.PostForm("name"))
	sourceType := strings.TrimSpace(c.PostForm("type"))
	status := strings.TrimSpace(c.PostForm("status"))
	sortValue := parseIntOr(c.PostForm("sort"), 0)
	req := &inventorydto.UpdateSourceReq{
		ID:           c.PostForm("id"),
		Code:         &code,
		Name:         &name,
		Type:         &sourceType,
		Status:       &status,
		Sort:         &sortValue,
		RelatedParty: sourceRelatedForm(c.PostForm("relatedParty")),
		Config:       sourceConfigForm(c.PostForm("config")),
	}
	// 表单是最终状态：留空即「这条货源没有结算价」，显式交给 service 清空。
	price, perr := sourceSettleForm(c.PostForm("settlePrice"))
	if perr != nil {
		h.sourceFormFail(c, true, perr)
		return
	}
	if price == nil {
		req.ClearSettlePrice = true
	} else {
		req.SettlePrice = price
	}
	if _, err := h.inventory.UpdateSource(c.Request.Context(), req); err != nil {
		h.sourceFormFail(c, true, err)
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventorySourceBack(c), inventorySourceBackText(c))
}

// InventorySourceDelete 删除货源。
func (h *inventorySourcePageHandle) InventorySourceDelete(c *gin.Context) {
	if err := h.inventory.DeleteSource(c.Request.Context(), &inventorydto.DeleteSourceReq{
		ID: c.PostForm("id"),
	}); err != nil {
		inventoryJump(c, false, inventoryErrText(c, err), inventorySourceBack(c), inventorySourceBackText(c))
		return
	}
	inventoryJump(c, true, inventoryDoneText(c), inventorySourceBack(c), inventorySourceBackText(c))
}

// InventorySourcesBulkDelete 批量删除货源。
//
// 逐条走同一条删除路径：被采购单等历史数据引用的那一条由服务端拒绝，其余照常删除 ——
// 批量操作不能因为一条失败就整批回滚（用户会以为「一条都没删」，然后反复重试）。
// 结果按「已删 N 个 / 跳过 M 个」渲染成提示页，避免静默的部分成功（语义未变，只换传输通道）。
func (h *inventorySourcePageHandle) InventorySourcesBulkDelete(c *gin.Context) {
	back := inventorySourceBack(c)
	backText := inventorySourceBackText(c)
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 超限是 shell 的**受控错误**（shell.BulkIDsError：值域只有 Count/Max 两个整数，
		// 装不下表名 / 约束名 / SQLSTATE），文案走 shell 的受控出口 —— 它按当前语言拼出
		//「一次最多操作 N 项，当前 M 项，请分批进行」，不会把可行动提示抹成通用提示。
		inventoryJump(c, false, shell.BulkIDsFacingText(c, berr), back, backText)
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.inventory.DeleteSource(c.Request.Context(), &inventorydto.DeleteSourceReq{ID: id}); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	switch {
	case skipped > 0:
		inventoryJump(c, false, fmt.Sprintf(
			inventoryBulkText(c, inventoryBulkSourcePartial), strconv.Itoa(deleted), strconv.Itoa(skipped)), back, backText)
	case deleted > 0:
		inventoryJump(c, true, fmt.Sprintf(
			inventoryBulkText(c, inventoryBulkSourceDone), strconv.Itoa(deleted)), back, backText)
	default:
		// 没有选中任何 id（批量条在无勾选时不会提交）：回列表，不渲染提示页。
		redirectWhere(c, back)
	}
}

// sourceSummary 关联方统计（验收 4）。统计失败不阻断页面：置空并附带提示。
// c 是给「错误文案三件套」用的（翻译 + 记日志时的 user_id）：取数失败要走 inventoryErrText，
// 而不是把 err.Error() 铺进模板。
func (h *inventorySourcePageHandle) sourceSummary(c *gin.Context, ctx context.Context, projectID string, pageErr *string) (out gin.H, ok bool) {
	out = gin.H{"Groups": []gin.H{}}
	if projectID == "" {
		return out, false
	}
	res, err := h.inventory.SourceSummary(ctx, &inventorydto.SourceSummaryReq{ProjectID: projectID})
	if err != nil {
		// 同上：跨模块取数失败也可能是基础设施错误（表名 / SQLSTATE），走同一套归口，
		// 绝不用 err.Error() 直接铺到页面上。
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, err)
		}
		return out, false
	}
	groups := make([]gin.H, 0, len(res.Groups))
	tr := shell.TranslateFor(c)
	for _, g := range res.Groups {
		groups = append(groups, gin.H{
			"Type": g.Type, "TypeLabel": sourceTypeLabel(tr, g.Type),
			"RelatedParty": g.RelatedParty, "RelatedPartyLabel": sourceRelatedLabel(tr, g.RelatedParty),
			"Count": g.Count,
		})
	}
	return gin.H{
		"Total": res.Total, "Internal": res.Internal, "External": res.External,
		"RelatedParty": res.RelatedParty, "Unrelated": res.Unrelated,
		"SettlePriced": res.SettlePriced, "Groups": groups,
	}, true
}

// sourceRow 货源行 → 模板视图（金额与配置都先格式化，模板不做逻辑）。
func sourceRow(tr func(key, fallback string) string, s *inventorydto.SourceResp) gin.H {
	settlePrice := "—"
	if s.SettlePrice != nil {
		settlePrice = strconv.FormatFloat(*s.SettlePrice, 'f', 2, 64)
	}
	return gin.H{
		"ID": s.ID, "Code": s.Code, "Name": s.Name,
		"Type": s.Type, "TypeLabel": sourceTypeLabel(tr, s.Type),
		"RelatedParty": s.RelatedParty, "RelatedPartyLabel": sourceRelatedLabel(tr, s.RelatedParty),
		"Status": s.Status, "StatusLabel": statusLabel(tr, s.Status),
		"SettlePrice": settlePrice, "HasSettlePrice": s.SettlePrice != nil,
		"Config": prettyJSON(s.Config), "Sort": s.Sort, "UpdatedAt": s.UpdatedAt,
	}
}

// sourceTypeOptions 类型下拉（外部 / 内部）。
func sourceTypeOptions(tr func(key, fallback string) string) []gin.H {
	return []gin.H{
		{"Value": inventoryenums.SourceTypeExternal, "Label": sourceTypeLabel(tr, inventoryenums.SourceTypeExternal)},
		{"Value": inventoryenums.SourceTypeInternal, "Label": sourceTypeLabel(tr, inventoryenums.SourceTypeInternal)},
	}
}

// sourceStatusOptions 状态下拉（启用 / 停用）。
func sourceStatusOptions(tr func(key, fallback string) string) []gin.H {
	return []gin.H{
		{"Value": inventoryenums.SourceStatusActive, "Label": statusLabel(tr, inventoryenums.SourceStatusActive)},
		{"Value": inventoryenums.SourceStatusDisabled, "Label": statusLabel(tr, inventoryenums.SourceStatusDisabled)},
	}
}

// sourceRelatedOptions 关联方三态下拉：空值 = 按类型默认（编辑时 = 不改）。
func sourceRelatedOptions(tr func(key, fallback string) string) []gin.H {
	return []gin.H{
		{"Value": "", "Label": tr(inventoryenums.InventorySourcesRelatedTypeDefault, "按类型默认 / 不改")},
		{"Value": "true", "Label": sourceRelatedLabel(tr, true)},
		{"Value": "false", "Label": sourceRelatedLabel(tr, false)},
	}
}

// sourceTypeLabel 货源类型 → 当前语言展示文案。
func sourceTypeLabel(tr func(key, fallback string) string, sourceType string) string {
	switch sourceType {
	case inventoryenums.SourceTypeInternal:
		return tr(inventoryenums.InventorySourcesTypeInternal, "内部（集团内 / 自家工厂）")
	case inventoryenums.SourceTypeExternal:
		return tr(inventoryenums.InventorySourcesStatsExternal, "外部供应商")
	default:
		return sourceType
	}
}

// sourceRelatedLabel 关联方标志 → 当前语言展示文案。
func sourceRelatedLabel(tr func(key, fallback string) string, related bool) string {
	if related {
		return tr(inventoryenums.InventorySourcesStatsRelated, "关联方")
	}
	return tr(inventoryenums.InventorySourcesStatsUnrelated, "非关联方")
}

// sourceRelatedForm 解析关联方下拉：空串 = 未指定（新建按类型默认 / 编辑不改）。
func sourceRelatedForm(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		yes := true
		return &yes
	case "false", "0":
		no := false
		return &no
	default:
		return nil
	}
}

// sourceSettleForm 解析结算价输入：空串 = 无值（更新路径据此清空）。
func sourceSettleForm(value string) (out *float64, err error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil, nil
	}
	parsed, perr := strconv.ParseFloat(raw, 64)
	if perr != nil {
		return nil, errors.New(inventoryenums.ErrSourceSettleInvalid)
	}
	return &parsed, nil
}

// sourceConfigForm 解析对接配置输入：空串表示未填（service 落成 {}）。
func sourceConfigForm(value string) json.RawMessage {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	return json.RawMessage(raw)
}

// prettyJSON 把 jsonb 缩进成可读文本（解析失败时原样回填，不吞掉用户数据）。
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

var sourceCreateFields = []string{"projectId", "code", "name", "type", "relatedParty", "settlePrice", "sort", "config"}
var sourceEditFields = []string{"projectId", "id", "code", "name", "type", "relatedParty", "status", "settlePrice", "sort", "config"}

func sourceFormData(c *gin.Context, edit bool, row gin.H) gin.H {
	tr := shell.TranslateFor(c)
	return gin.H{
		"IsEdit": edit, "Row": row, "SelectedProject": c.PostForm("projectId"),
		"TypeOptions": sourceTypeOptions(tr), "RelatedOptions": sourceRelatedOptions(tr),
		"StatusOptions": sourceStatusOptions(tr),
	}
}

func (h *inventorySourcePageHandle) sourceFormFail(c *gin.Context, edit bool, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryJump(c, false, inventoryErrText(c, err), inventorySourceBack(c), inventorySourceBackText(c))
		return
	}
	// htmx 分支原样保留：失败在抽屉里重渲片段。
	fields := sourceCreateFields
	if edit {
		fields = sourceEditFields
	}
	data := sourceFormData(c, edit, nil)
	data["FormEcho"] = inventoryRawFormValues(c, fields)
	data["SubmitErr"] = inventoryErrText(c, err)
	data["ListQuery"] = inventoryQueryFromRequest(c, inventorySourceBackKeys...)
	c.HTML(http.StatusOK, "admin/inventory/inventory_source_form.html", shell.Prepare(c, data))
}
