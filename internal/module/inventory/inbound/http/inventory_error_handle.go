// 库存页的错误文案与整页失败视图。
//
// 两块内容：业务错误 → 事实（key + 兜底）→ 当前语言文字（判定只有一条 pkg/facing）；
// 整页 GET 失败交回一个**视图**（inventoryLoadFailedView），状态码与缓存策略由渲染层读
// view.Result —— 控制器里没有 c.HTML，也没有第二个出口函数。
package inventoryhttp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go_wp/internal/shell"
	"go_wp/internal/text"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"strconv"
	"strings"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	bulkids "go_wp/pkg/bulkids"
)

// inventoryErrScene 结构化日志的场景名（与模块其它 logger.Scene("inventory") 调用点一致）。
const inventoryErrScene = "inventory"

// inventoryErrMsg 业务错误 → 一句结论的**事实**（key + 中文兜底 + 参数）。
//
// 判据与归口只有一份实现：text.FactOf → pkg/response.IsBusinessError（形态判据，
// 与 JSON 出口 response.ErrorAuto 同一份）。本模块原先另持一份文案 key 白名单
// （inventoryErrKeys，约 70 行），那是**第二份真相**：enums 新增一条业务错误时要记得在两处
// 登记，而漏登记的方向还是「静默变成系统内部错误」—— 用户看到一句不可行动的提示，页面上
// 看不到任何线索。白名单已删除；模块侧只剩**兜底表**（inventoryErrFallbacks：key → 中文原文），
// 它只在词条缺失 / i18n 未初始化时生效，不是可透出性判据。
//
// 非业务错误对外只给共享归口文案（text.InternalMsg），**原文进结构化日志**
// （PageErrorFact 带场景 + path + user_id）—— 「不许直出内部错误」之后仍要留下可诊断性：
// 谁在哪个页面撞上了哪条 SQL / 约束，只有日志能回答。
func inventoryErrMsg(c *gin.Context, err error) facing.Msg {
	return inventoryErrMsgOf(c, text.FactOf(c, inventoryErrScene, err))
}

// inventoryErrMsgOf 把错误事实摊成一句结论。三档：
//
//	· Key 为空（err 为 nil）→ 空事实（提示页那边会把空正文落成归口文案）；
//	· 非业务错误 → 共享归口事实（key + 中文原文）；
//	· 业务错误 → key + 模块兜底表的中文；带明细段时给成品串 ——「key + 明细」一段 key
//	  表达不了（明细是多段「词条 + 具名参数」的编码）。
func inventoryErrMsgOf(c *gin.Context, f facing.Fact) facing.Msg {
	if f.Key == "" {
		return facing.Msg{}
	}
	if !f.Business {
		return text.InternalMsg()
	}
	if f.Tail != "" {
		if detail := inventoryErrDetailText(shell.TranslateFor(c), f.Tail); detail != "" {
			// 补充说明已按当前语言渲染：service 写进去的是「为什么被拒」的上下文
			//（例如被哪个商品占了外码），吞掉它就等于让人去猜。
			return facing.Msg{Fallback: text.TranslateArgs(c, f.Key, inventoryErrFallback(f.Key), nil) + "：" + detail}
		}
	}
	return facing.Msg{Key: f.Key, Fallback: inventoryErrFallback(f.Key)}
}

// inventoryFailDrawer 抽屉 / 片段写动作的失败出口：**4xx + JSON**，不重渲片段。
//
// 为什么不是 200 + 重渲片段：htmx 对 2xx 一律 swap 到目标节点，重渲意味着「服务端得把
// 用户刚输入的值原样搬回来」—— 一套平行的回填通道（EchoFields / RawForm / 片段里的
// FormEcho 分支），每个字段三处对齐，漏一处就静默丢输入。而 htmx 对 4xx **不 swap**：
// 抽屉保持打开、输入原样在，响应只需要一句话，由 admin.js 的 htmx:responseError 取
// body.message 弹 toast。所以这里既不渲染 HTML，也不回 200（200 会被当成功并替换节点）。
//
// 状态码分档与整页失败同一份判据（pkg/response.IsBusinessError）：业务错误 400、
// 其余 500；对外文案走 inventoryErrText（业务错误取词 + 兜底，内部错误只给归口文案，
// 原文进结构化日志）。
func inventoryFailDrawer(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if response.IsBusinessError(err) {
		status = http.StatusBadRequest
	}
	response.ErrorWithMessage(c, status, inventoryErrText(c, err))
}

// inventoryErrText 一句结论的**文本**形态（片段槽位 / 页面提示条用：那边的模板直接渲染文本）。
//
// 这些槽位本批还没取词化（模板拿到的是成品文本而不是「key + 兜底」，因此它无法按请求语言
// 自己取词）—— 清单见本批报告。新槽位一律传 facing.Msg 并在模板里用 tf 取词。
func inventoryErrText(c *gin.Context, err error) string {
	return inventoryMsgText(c, inventoryErrMsg(c, err))
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
// tail 由 text.FactOf 按 **ASCII 冒号** 从「业务 key: 明细」里切出来
// （见 pkg/response.IsBusinessError 的锚定形态判据）；写成全角「：」时切不开，
// 整条错误会被判成内部故障、明细随之一并丢掉。
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
// 渲染提示页，见 inventory_notice_handle.go），所以下面这套「证明提示出自本仓」的读侧判定整批删除：
// inventoryPageErr / inventoryPageOk / inventoryPageDone / inventoryNoticeSuccess /
// inventoryNoticeTexts / inventoryNoticeToken / inventoryBulkNoticeTemplates。
//
// 留下的只有**事实**：单条回执与批量结论的 key + 中文兜底（facing.Msg）。
// 取词不再是本文件的事 —— 它在提示页渲染期由模板注入的 tf 完成（见 internal/adminui/prepare.go），
// 所以「成功说的是哪句话」与「这是哪门语言」在两处各自回答一次，不再混在调用点里。

// inventoryActionDoneKey / inventoryActionDoneFallback 单条写动作成功回执的 i18n key 与中文兜底。
//
// 词条见迁移 280（中英各一行）；取不到词条时回落这份中文（与仓库里其它
// 「key + 中文兜底」的写法一致），中文环境的表现就是「操作已完成」。
// 出口处的载体是 inventory_notice_handle.go 的 inventoryDoneMsg（facing.Msg）。
const (
	inventoryActionDoneKey      = inventoryenums.InventoryActionDone
	inventoryActionDoneFallback = "操作已完成"
)

// —— 批量结论（事实形态：key + 中文兜底 + `%s` 参数）——
//
// 原先这里是「模板结构体 inventoryBulkNoticeTemplate + inventoryBulkText 取词 + 调用点
// fmt.Sprintf」的三件套：取词、语言判定、拼串都在 Go 侧完成，调用点还要自己记住
// 「这条词条是 %d 还是 %s」的口径。收成 facing.Msg 之后，调用点只给 strconv 过的数字，
// 取词与占位符填充都在渲染期由模板注入的 tf 完成（同 text.TranslateArgs 一份实现）。
//
// 兜底一律写 `%s`：TranslateArgs 只接受 `%s` 位置占位符，词条里混进 `%d` 时它会**丢弃词条**
// 回落这里的兜底，而兜底若还是 `%d`、参数却是字符串，Sprintf 会产出 `%!d(string=2)` 这种
// 「看起来像结论」的垃圾。迁移 243 里 admin.inventory.bulk.partial / .deleted 两条词条
// 正是 `%d`（中英都是），它们需要一份把占位符改成 `%s` 的 i18n 迁移 —— 见本批报告。
// 在那之前：中文界面输出逐字不变（词条与兜底同文），英文界面这两条会落中文兜底。
var (
	inventoryBulkWarehousePartial   = facing.Msg{Key: inventoryenums.InventoryBulkWarehousePartial, Fallback: "已删除 %s 个，%s 个未能删除（默认仓或仓内仍有非零库存）"}
	inventoryBulkWarehouseDone      = facing.Msg{Key: inventoryenums.InventoryBulkWarehouseDone, Fallback: "已删除 %s 个仓库"}
	inventoryBulkSourcePartial      = facing.Msg{Key: inventoryenums.InventoryBulkSourcePartial, Fallback: "已删除 %s 个，%s 个未能删除（仍被采购单或历史流水引用）"}
	inventoryBulkSourceDone         = facing.Msg{Key: inventoryenums.InventoryBulkSourceDone, Fallback: "已删除 %s 个货源"}
	inventoryBulkReasonPartial      = facing.Msg{Key: inventoryenums.InventoryBulkReasonPartial, Fallback: "已更新 %s 个原因，%s 个未能更新"}
	inventoryBulkReasonDone         = facing.Msg{Key: inventoryenums.InventoryBulkReasonDone, Fallback: "已更新 %s 个原因"}
	inventoryBulkReasonNoneSelected = facing.Msg{Key: inventoryenums.InventoryBulkReasonNoneSelected, Fallback: "请选择要操作的原因"}
)

// inventoryBulkIDsMsg 批量 id 读取失败的一句结论（事实形态，提示页出口用）。
//
// 判据是**类型事实**（*bulkids.IDsError）而不是文案比对：超限文案带两个只有错误类型知道的
// 数字（上限 Max 与**去重后**的本次条数 Count），拿 bulkids.MaxIDs 重算就丢了 Count。
// 与 adminui.BulkIDsFacingText 同一判据、同一份词条 —— 差别只在这里交出去的是「key + 参数」
// 而不是拼好的字符串（提示页要的是事实）。
func inventoryBulkIDsMsg(c *gin.Context, err error) facing.Msg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		// 未命中也接住：调用方几乎总是把批量读取的错误直接递进来，但判据必须是类型而不是
		// 「谁构造的」—— 换成别的错误时应当退回归口文案，而不是把不认识的原文送出去。
		if err != nil {
			logger.Scene(inventoryErrScene).
				With("path", c.Request.URL.Path).
				Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
		}
		return text.InternalMsg()
	}
	logger.Scene(inventoryErrScene).
		With("path", c.Request.URL.Path).
		With("count", tooMany.Count).
		With("max", tooMany.Max).
		Warn("批量操作被拒：提交的 id 数量超过上限")
	return facing.Msg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
}

// inventoryErrFallback 取兜底文案：跟**错误声明处**走（inventoryenums 的 errFallbacks）。
//
// 这里不再有本模块的第二张表：enums 的 errFallbacks 与常量一一对账（inventory_enums_test.go），
// 加了常量却忘了兜底会先在那条测试上变红，而不是等到某个页面把 ErrReceiptOrderDone 摆给运营看。
// 未登记时退回 key —— 至少还能与 sys_i18n 里的词条对照。
func inventoryErrFallback(key string) string {
	if text := inventoryenums.FallbackFor(key); text != "" {
		return text
	}
	return key
}

// inventoryPageMeta 一个库存页的身份：失败视图所需的侧栏高亮项与页面标题。
//
// 标题走 inventoryenums 常量（与正常渲染同一份 key，不在两处各写字面量）。
// 模板名不在这里：它属于路由装配（见 inventory_page_router.go 的三个 Template 常量）——
// 失败与成功渲染**同一张模板**，控制器不该再持有一份。
type inventoryPageMeta struct {
	titleKey      string
	titleFallback string
	menu          string
}

var (
	inventoryMetaHome = inventoryPageMeta{
		titleKey: inventoryenums.InventoryTitle, titleFallback: "库存管理",
		menu: "inventory",
	}
	inventoryMetaWarehouses = inventoryPageMeta{
		titleKey: inventoryenums.InventoryWarehousesTitle, titleFallback: "仓库管理",
		menu: "inventory-warehouses",
	}
	inventoryMetaReasons = inventoryPageMeta{
		titleKey: inventoryenums.InventoryReasonsTitle, titleFallback: "变动原因字典",
		menu: "inventory-reasons",
	}
)

// inventoryLoadFailedView 取数失败时的整页视图：同一张模板 + 全部键的零值 + 失败槽。
//
// 它是**视图**，不是错误出口：状态码与缓存策略由 PageStatus / PageNoStore 交给渲染层
// （adminui.Page 在路由装配处读 view.Result），控制器里没有 c.HTML、没有状态码分档。
// 500 + no-store 的理由没变：200 会让失败在监控里与正常页无从区分，且「重试」会从缓存
// 里拿回同一份失败。
type inventoryLoadFailedView gin.H

func (inventoryLoadFailedView) PageStatus() int   { return http.StatusInternalServerError }
func (inventoryLoadFailedView) PageNoStore() bool { return true }

// inventoryPageErrorData 装载失败时的键集：全部零值 + 失败槽。
//
// 零值而不是「顺手再查一把」：主数据都没读到，任何派生数据都不该再下探 —— 工程上下文没定下来时，
// 拿 URL 里那个 project 去查仓库 / 流水，查出来的是哪个工程的东西都说不清（与采购页 loadFailed 后的守卫同一判据）。
//
// 键是三个页面的超集：Jet 的缺失键是**整页中断**，失败页缺一个键就是「又一个坏页面」；
// 多给的键不渲染，没有副作用。
func inventoryPageErrorData(c *gin.Context, meta inventoryPageMeta, err error) inventoryLoadFailedView {
	return inventoryLoadFailedView{
		"title": inventoryMsgText(c, facing.Msg{Key: meta.titleKey, Fallback: meta.titleFallback}),
		"menu":  meta.menu,
		// 页头里的工程上下文选择器读 .Projects（len + range）：零值 = 不渲染下拉。
		"Projects":        []projectcontract.ProjectResp{},
		"SelectedProject": "",
		// 三个页面的顶层 let 与实际读取的键（inventory.html 的 reasons / selectedVariant /
		// selectedSKU、仓库页的类型下拉与新建抽屉、原因页的原因抽屉……）。
		"SelectedVariant":     "",
		"SelectedSKU":         "",
		"Warehouses":          []inventorydto.WarehouseRowResp{},
		"WarehouseTypes":      []gin.H{},
		"WarehouseCreateForm": gin.H{},
		"VariantOptions":      []gin.H{},
		"StockRows":           []inventorydto.StockByWarehouseRowResp{},
		"Reasons":             []inventorydto.ReasonRowResp{},
		"AdjustReasons":       []inventorydto.ReasonRowResp{},
		"AdjustDirections":    []inventorydto.OptionResp{},
		"Directions":          []inventorydto.OptionResp{},
		"Movements":           []inventorydto.MovementRowResp{},
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