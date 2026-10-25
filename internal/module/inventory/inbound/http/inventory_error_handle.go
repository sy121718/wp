// 库存页的错误文案与整页失败视图。
//
// 两块内容：业务错误 → 事实（key + 兜底）→ 当前语言文字（判定只有一条 pkg/facing）；
// 整页 GET 失败交回一个**视图**（inventoryLoadFailedView），状态码与缓存策略由渲染层读
// view.Result —— 控制器里没有 c.HTML，也没有第二个出口函数。
package inventoryhttp

import (
	"errors"

	"go_wp/internal/shell"
	"go_wp/internal/text"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	inventoryenums "go_wp/internal/module/inventory/enums"
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
//	· Key 为空（err 为 nil）→ 空事实（出口那边会把空正文落成归口文案）；
//	· 非业务错误 → 共享归口事实（key + 中文原文）；
//	· 业务错误 → key + 模块兜底表的中文；带明细段时给成品串 ——「key + 明细」一段 key
//	  表达不了（明细是多段「词条 + 具名参数」的编码）。
func inventoryErrMsgOf(c *gin.Context, f facing.Fact) facing.Msg {
	if f.Key == "" {
		return facing.Msg{}
	}
	if !f.Business {
		return response.InternalMsg()
	}
	if f.Tail != "" {
		if detail := inventoryErrDetailText(shell.TranslateFor(c), f.Tail); detail != "" {
			// 补充说明已按当前语言渲染：service 写进去的是「为什么被拒」的上下文
			//（例如被哪个商品占了外码），吞掉它就等于让人去猜。
			return facing.Msg{Fallback: response.ArgsText(c, f.Key, inventoryErrFallback(f.Key), nil) + "：" + detail}
		}
	}
	return facing.Msg{Key: f.Key, Fallback: inventoryErrFallback(f.Key)}
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
// 写动作的结论不再经 `?err=` / `?ok=` / `?done=` 回列表页（改由响应体承载：成功回列表、失败 JSON
// 由响应体承载，见 inventory_notice_handle.go），所以下面这套「证明提示出自本仓」的读侧判定整批删除：
// inventoryPageErr / inventoryPageOk / inventoryPageDone / inventoryNoticeSuccess /
// inventoryNoticeTexts / inventoryNoticeToken / inventoryBulkNoticeTemplates。
//
// 留下的只有**事实**：单条回执与批量结论的 key + 中文兜底（facing.Msg）。
// 取词不再是本文件的事 —— 它在出口处按请求语言取（text.MsgText / response.MessageOf），
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

// inventoryBulkIDsMsg 批量 id 读取失败的一句结论（事实形态，失败出口用）。
//
// 判据是**类型事实**（*bulkids.IDsError）而不是文案比对：超限文案带两个只有错误类型知道的
// 数字（上限 Max 与**去重后**的本次条数 Count），拿 bulkids.MaxIDs 重算就丢了 Count。
// 与 adminui.BulkIDsFacingText 同一判据、同一份词条 —— 差别只在这里交出去的是「key + 参数」
// 而不是拼好的字符串（出口要的是事实）。
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
		return response.InternalMsg()
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

// 页面 GET 取数失败走与写动作**同一个出口**（inventoryFail，见 inventory_notice_handle.go）：
// 状态码由错误自己声明（pkg/facing.Statused），文案走 inventoryErrText，与写动作同一份事实。
//
// 为什么不另建一个 pageLoadFailed 助手：那会是模块里的第三个出口，而它要做的判断与
// inventoryFail 一字不差 —— 复制一份判据等于给自己留一条静默漂移的路（门禁
// check-shell-migration.sh 的 ownhelper 计数就是拦这件事的）。
//
// 失败不渲染页壳（用户裁定：所有提示只需要一个提示）：从前这里交回一个
// `inventoryLoadFailedView`（gin.H + PageStatus/PageNoStore 出口包装），代价是同一张模板
// 要维护两套必需键 —— 那个函数下面原来那张 20 多个零值键的表就是那份代价。失败时页壳给的
// 东西（侧栏、工程下拉、筛选栏）没有一条是这次失败需要的：主数据都没读到，
// 拿 URL 里那个 project 去渲染下拉，显示的是哪个工程的东西都说不清。
