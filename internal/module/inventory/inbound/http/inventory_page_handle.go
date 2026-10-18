// 库存管理页：库存总览、流水筛选、分页，以及页面级共享助手（回跳、HX 判定、词条标签）。
package inventoryhttp

import (
	"fmt"
	"go_wp/internal/shell"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
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
	products productcontract.VariantOptionsReader
}

// NewInventoryPageHandle 构造。
//
// products 只用于「商品 → 变体」下拉：装配期传入 nil，装配收尾由 SetProductCatalog 补注
// （商品模块与本模块互为依赖，装配顺序上无法在此时拿到商品契约）。
func NewInventoryPageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService, products productcontract.VariantOptionsReader) *inventoryPageHandle {
	return &inventoryPageHandle{inventory: inventory, projects: projects, products: products}
}

// setProductCatalog 装配期补注商品契约（见 inventory_router.go 的 SetProductCatalog）。
func (h *inventoryPageHandle) setProductCatalog(products productcontract.VariantOptionsReader) {
	h.products = products
}

// InventoryPage 库存管理页：工程切换 + 流水筛选 + 某 SKU 各仓库存（只读） + 调整入口。
func (h *inventoryPageHandle) InventoryPage(c *gin.Context) (any, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return inventoryPageErrorData(c, inventoryMetaHome, err), nil
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	warehouses, err := h.listWarehouses(c, selected)
	if err != nil {
		return inventoryPageErrorData(c, inventoryMetaHome, err), nil
	}
	options, err := h.variantOptions(ctx, selected)
	if err != nil {
		return inventoryPageErrorData(c, inventoryMetaHome, err), nil
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
	stockRows := []inventorydto.StockByWarehouseRowResp{}
	if sku != "" {
		rows, serr := h.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
			ProjectID: selected, SKUCode: sku,
		})
		if serr != nil {
			return inventoryPageErrorData(c, inventoryMetaHome, serr), nil
		}
		tr := shell.TranslateFor(c)
		byWarehouse := make(map[string]*inventorydto.StockResp, len(rows))
		for _, r := range rows {
			byWarehouse[r.WarehouseID] = r
		}
		for _, w := range warehouses {
			r := byWarehouse[w.ID]
			if r == nil {
				// 未入库：该仓没有这条 SKU 的库存行。没有可编辑的对象，
				// 因此数量列给提示、行内表单不渲染（表单要定位到一行）。
				stockRows = append(stockRows, inventorydto.StockByWarehouseRowResp{
					WarehouseID: w.ID, Present: false,
					WarehouseName: w.Name, WarehouseCode: w.Code,
					SKUCode:       sku,
					QuantityLabel: tr(inventoryenums.InventoryStockNotStocked, "未入库"),
				})
				continue
			}
			// 字段含义见 inventorydto.StockByWarehouseRowResp（WarehouseID 是行内编辑的定位键，
			// ExternalSKU 空串合法，TrackQuantity 与 Quantity 必须成对给出）。
			stockRows = append(stockRows, inventorydto.StockByWarehouseRowResp{
				WarehouseID: w.ID, Present: true,
				WarehouseName: r.WarehouseName, WarehouseCode: r.WarehouseCode,
				VariantID: r.VariantID, SKUCode: r.SKUCode,
				ExternalSKU:   r.ExternalSKU,
				TrackQuantity: r.TrackQuantity, Quantity: r.Quantity,
				QuantityLabel: stockQuantityLabel(tr, r.TrackQuantity, r.Quantity),
				UpdatedAt:     r.UpdatedAt,
			})
		}
	}

	reasons, err := h.listReasons(c, selected)
	if err != nil {
		return inventoryPageErrorData(c, inventoryMetaHome, err), nil
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
		return inventoryPageErrorData(c, inventoryMetaHome, err), nil
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
		// 读回）：写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 inventory_notice_handle.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventoryStockBackKeys...),
	}
	// 分页条（shell 组件，服务端渲染）：基址带当前全部筛选维度（工程 / SKU / 变体 / 仓 /
	// 方向 / 原因 / 时间区间），翻页时不丢条件 —— 丢了条件会让人以为「记录变多了」，
	// 实际是筛选被清掉。
	// total 来自契约的 CountMovements（与 ListMovements 同一份过滤条件），因此分页条给的是
	// 真页码窗口与「共 N 条」，而不是「上一页 / 下一页 + 后面还有记录」这种探测式降级形态。
	// 单页或空数据时 HasPagination 为假，模板自然不渲染。
	//
	// 分页条只交事实：信息行 / 页码窗口 / 链接参数顺序由 partial 现算。
	if pg := pagination.Facts(total, page, limit); pg.HasPagination() {
		pg.Base = reqparams.FilterBaseURL(inventoryPagePath, map[string]string{
			"project":     selected,
			"sku":         sku,
			"variantId":   variantID,
			"warehouseId": filterWarehouse,
			"direction":   filterDirection,
			"reasonCode":  filterReason,
			"timeFrom":    filterTimeFrom,
			"timeTo":      filterTimeTo,
		})
		pageData["Pagination"] = pg
	}
	return pageData, nil
}

// listMovements 库存流水列表（按 SKU / 仓库 / 方向 / 原因 / 时间过滤 + 分页）。
//
// total 是该过滤条件下的**真实总条数**（契约的 CountMovements，与 ListMovements 同一份
// 过滤条件）；curPage 是**收敛后**的页码（page 越界时回落到末页）。out 只装本页数据。
func (h *inventoryPageHandle) listMovements(c *gin.Context, projectID, sku, warehouseID,
	direction, reasonCode, timeFrom, timeTo string, page, limit int) (out []inventorydto.MovementRowResp, total int64, curPage int, err error) {
	ctx := c.Request.Context()
	out = []inventorydto.MovementRowResp{}
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
		out = append(out, inventorydto.MovementRowResp{
			ID: m.ID, SKUCode: m.SKUCode, WarehouseName: m.WarehouseName,
			WarehouseCode: m.WarehouseCode, Direction: m.Direction,
			DirectionLabel: directionLabel(tr, m.Direction),
			Quantity:       m.Quantity, QuantityBefore: m.QuantityBefore, QuantityAfter: m.QuantityAfter,
			ReasonName: tr(m.ReasonName, m.ReasonCode), ReasonCode: m.ReasonCode,
			SourceType: m.SourceType, SourceRef: m.SourceRef, Remark: m.Remark,
			OperatorID: m.OperatorID, BatchID: m.BatchID, CreatedAt: m.CreatedAt,
		})
	}
	return out, total, curPage, nil
}

// —— 列表分页（库存流水 / 货源 / 采购入库三页共用）——
//
// 三页原先都是「handler 里写死上限 + 模板里没有分页条」：流水 50 条、货源 200 条、
// 采购单 100 条，第 N+1 条起**静默消失**（流水按时间倒序，第 51 条之后的老记录永远看不到）。
//
// 这里接的是项目既有的分页设施（`partials/pagination.html` + pkg/pagination 事实层 Facts），
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
// （pkg/pagination 只收敛分页条自己显示的那一页，不会回头改取数用的页码）。
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

// outstandingTone 采购行「未入库数量」→ 徽标分档。
//
// 还有没收的量 → warn（要人去收货）；收满 → ok（这一行不用管了）。
// 判据与同一行的 Open 同源（都来自 OutstandingQuantity）：按钮的可用性与徽标的档位
// 不该各判一次 —— 两处判据分叉就会出现「按钮说还能收、徽标说收满了」。
func outstandingTone(outstanding int) string {
	if outstanding > 0 {
		return "warn"
	}
	return "ok"
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

// —— 错误文案（回跳地址与提示页出口见 inventory_notice_handle.go）——
//
// 原先这一节还有 inventoryURL / inventoryConclusion / inventoryErrURL 三个函数：写动作的
// 结论经 302 + `?err=` / `?ok=` / `?done=` 回列表页，回跳地址由 Go 拼。改成 shell.RenderJump
// 渲染提示页之后，**结论不再进 URL**，那套「拼 URL + 读侧白名单」整批删除。

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
//	· 原生请求：失败渲染整页提示（inventoryJump / shell.RenderJump，见 inventory_notice_handle.go）——
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
// 判据委托 reqparams.IsHXRequest（同一份实现）：RenderJump 对 htmx 输出 HX-Redirect 用的也是它，
// 两个出口必须认同同一个信号 —— 各写一份就会出现「某个模块的失败分档静默失效」。
func isHXRequest(c *gin.Context) bool {
	return reqparams.IsHXRequest(c)
}
