package inventoryhttp

// inventory_err.go — 整页 GET 的失败出口（库存管理 / 仓库管理 / 变动原因字典三页）。
//
// 判据见 docs/rules/template-boundary.md §「整页 GET 的失败出口：失败也必须是『这一页』」：
// 整页 GET 由浏览器发起导航，所以每一种结论 —— 读到、没读到 —— 都必须是同一个页面的完整 HTML。
// 这三页原先的取数失败走 shell.PageError → {code,message} JSON：浏览器停在 /admin/inventory 上，
// 页壳、侧栏、标题全丢，地址栏还停在原处；监控里失败与正常页也长得一模一样（同为 200 的序列）。
//
// 现在的形态与规范样板 navigation_page.go:290-317 一致：
//   · LoadFailed(bool) —— 事实位：本次请求**没读到**（不是「读到了、只是空的」）；
//   · LoadError(string) —— 非空即「这一页失败了」，内容是 inventoryErrText 交出的当前语言文字；
//   · 500 + Cache-Control: no-store —— 失败页不该进缓存，否则「重试」拿回的还是同一份失败；
//   · 同一模板 + adminui.Prepare —— 失败页也是「这一页」，页壳 / 侧栏 / 页头照旧；
//   · 原文只进日志（错误事实的判定已收敛到 adminui.PageErrorFact，见 inventoryErrMsg 的注释）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	inventoryenums "go_wp/internal/module/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
)

// inventoryPageMeta 一个库存页的身份：失败渲染所需的模板名、侧栏高亮项与页面标题。
//
// 三页共用一份失败出口，差异只剩这四个值 —— 各 handler 只传 meta 与 err。
// 标题走 inventoryenums 常量（与正常渲染同一份 key，不在两处各写字面量）。
type inventoryPageMeta struct {
	titleKey      string
	titleFallback string
	menu          string
	template      string
}

var (
	inventoryMetaHome = inventoryPageMeta{
		titleKey: inventoryenums.InventoryTitle, titleFallback: "库存管理",
		menu: "inventory", template: "admin/inventory/inventory.html",
	}
	inventoryMetaWarehouses = inventoryPageMeta{
		titleKey: inventoryenums.InventoryWarehousesTitle, titleFallback: "仓库管理",
		menu: "inventory-warehouses", template: "admin/inventory/inventory_warehouses.html",
	}
	inventoryMetaReasons = inventoryPageMeta{
		titleKey: inventoryenums.InventoryReasonsTitle, titleFallback: "变动原因字典",
		menu: "inventory-reasons", template: "admin/inventory/inventory_reasons.html",
	}
)

// inventoryPageErrorData 装载失败时的模板数据：全部零值 + 失败槽。
//
// 零值而不是「顺手再查一把」：主数据都没读到，任何派生数据都不该再下探 —— 工程上下文没定下来时，
// 拿 URL 里那个 project 去查仓库 / 流水，查出来的是哪个工程的东西都说不清（与采购页 loadFailed 后的守卫同一判据）。
//
// 键是三个页面的超集：Jet 的缺失键是**整页中断**，失败页缺一个键就是「又一个坏页面」；
// 多给的键不渲染，没有副作用。
//
// 失败槽与页头标题都是**文本槽位**（模板直接渲染文本、不取词），所以这里仍要把词取出来
// （inventoryMsgText / inventoryErrText 交出的成品文本）—— 这一类槽位本批还没取词化，
// 清单见本批报告；新槽位一律传 adminui.Msg 并在模板里用 tf 取词。
func inventoryPageErrorData(c *gin.Context, meta inventoryPageMeta, err error) gin.H {
	return gin.H{
		"title": inventoryMsgText(c, adminui.Msg{Key: meta.titleKey, Fallback: meta.titleFallback}),
		"menu":  meta.menu,
		// 页头里的工程上下文选择器读 .Projects（len + range）：零值 = 不渲染下拉。
		"Projects":        []projectcontract.ProjectResp{},
		"SelectedProject": "",
		// 三个页面的顶层 let 与实际读取的键（inventory.html 的 reasons / selectedVariant /
		// selectedSKU、仓库页的类型下拉与新建抽屉、原因页的原因抽屉……）。
		"SelectedVariant":     "",
		"SelectedSKU":         "",
		"Warehouses":          []gin.H{},
		"WarehouseTypes":      []gin.H{},
		"WarehouseCreateForm": gin.H{},
		"VariantOptions":      []gin.H{},
		"StockRows":           []gin.H{},
		"Reasons":             []gin.H{},
		"AdjustReasons":       []gin.H{},
		"AdjustDirections":    []gin.H{},
		"Directions":          []gin.H{},
		"Movements":           []gin.H{},
		"ReasonCreateForm":    gin.H{},
		"FilterWarehouse":     "",
		"FilterDirection":     "",
		"FilterReason":        "",
		"FilterTimeFrom":      "",
		"FilterTimeTo":        "",
		// 写动作表单 action 上的列表上下文：失败页不渲染表单，键照样存在（Jet 缺键即中断）。
		"ListQuery": "",
		// 失败槽：模板先判 LoadError、再判各页空态 —— 「没读到」与「读到了、只是空的」必须分开。
		"LoadFailed": true,
		"LoadError":  inventoryErrText(c, err),
	}
}

// inventoryPageError 三个库存页的整页失败出口：同一模板 + 500 + no-store。
//
// 关于「不新增出口 helper」（task-27 原文）：lead 已把它定为**窄例外** —— 不是禁令的重新解释，
// 也不是「计数不命中就算合规」。允许留下的理由只有一条：模板必需键 / 500 / no-store / 归口文案
// 这几条不变量必须只落一处；3 页 10 处各写一份时，漏一个键就是「又一个坏页面」。
//
// 三条边界照旧：不放进 shell（回的是「库存页的失败」，模板与数据形状都是本模块的）；
// 不渲染跳转页、不读回跳、不区分 htmx（它不是结论出口那族）；不被跨模块引用。
// 后续新增页面失败点请复用这里，不要再造第二个同级出口 helper，也不要靠改名规避门禁。
func inventoryPageError(c *gin.Context, meta inventoryPageMeta, err error) {
	c.Header("Cache-Control", "no-store")
	c.HTML(http.StatusInternalServerError, meta.template, adminui.Prepare(c, inventoryPageErrorData(c, meta, err)))
}
