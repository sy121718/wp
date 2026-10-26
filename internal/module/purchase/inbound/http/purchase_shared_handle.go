// purchase_shared_handle.go — 采购域后台页的共用件（错误文案 / 失败出口 / 回跳键表 / 分页）。
//
// 为什么自带一份而不是 import inventoryhttp：跨模块只经契约（module_boundary_test.go），
// inbound 层不跨模块复用。这几件原先都在库存侧，2026-10 采购独立成模块时随域搬来 ——
// 判据、兜底表、日志场景名都跟**错误声明处**走（purchaseenums），不假装还能共用。
package purchasehttp

import (
	"errors"
	"net/http"

	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	"go_wp/internal/shell"
	"go_wp/internal/text"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
)

// purchaseErrScene 结构化日志的场景名（与模块其它 logger.Scene("purchase") 调用点一致）。
const purchaseErrScene = "purchase"

// purchaseErrMsg 业务错误 → 一句结论的**事实**（key + 中文兜底 + 参数）。
//
// 判据与归口只有一份实现：text.FactOf → pkg/response.IsBusinessError（形态判据，
// 与 JSON 出口 response.ErrorAuto 同一份）。模块侧只持**兜底表**（purchaseenums 的
// errFallbacks），它只在词条缺失 / i18n 未初始化时生效，不是可透出性判据。
//
// 非业务错误对外只给共享归口文案（response.InternalMsg），原文进结构化日志
// （FactOf 带场景 + path + user_id）—— 「不许直出内部错误」之后仍要留下可诊断性。
func purchaseErrMsg(c *gin.Context, err error) facing.Msg {
	return purchaseErrMsgOf(c, text.FactOf(c, purchaseErrScene, err))
}

// purchaseErrMsgOf 把错误事实摊成一句结论。三档：
//
//	· Key 为空（err 为 nil）→ 空事实（出口那边会把空正文落成归口文案）；
//	· 非业务错误 → 共享归口事实（key + 中文原文）；
//	· 业务错误 → key + 模块兜底表的中文；带明细段时给成品串。
func purchaseErrMsgOf(c *gin.Context, f facing.Fact) facing.Msg {
	if f.Key == "" {
		return facing.Msg{}
	}
	if !f.Business {
		return response.InternalMsg()
	}
	if f.Tail != "" {
		if detail := purchaseErrDetailText(purchaseTranslate(c), f.Tail); detail != "" {
			return facing.Msg{Fallback: response.ArgsText(c, f.Key, purchaseErrFallback(f.Key), nil) + "：" + detail}
		}
	}
	return facing.Msg{Key: f.Key, Fallback: purchaseErrFallback(f.Key)}
}

// purchaseErrText 一句结论的**文本**形态（页面提示条用：模板直接渲染文本）。
func purchaseErrText(c *gin.Context, err error) string {
	return purchaseMsgText(c, purchaseErrMsg(c, err))
}

// purchaseErrDetailText 业务错误的**补充说明** → 当前语言文案。
//
// 明细词条的中文兜底跟声明处走（purchaseenums.ErrDetailFallbacks）；未登记的 key 跳过
// 该段并记一条日志（少一句补充说明，好过把编码串摆到页面上）。
func purchaseErrDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		return tail
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := purchaseenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(purchaseErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

// purchaseErrFallback 取兜底文案：跟**错误声明处**走（purchaseenums 的 errFallbacks）。
//
// 未登记时退回 key —— 至少还能与 sys_i18n 里的词条对照。
func purchaseErrFallback(key string) string {
	if fallback := purchaseenums.FallbackFor(key); fallback != "" {
		return fallback
	}
	return key
}

// purchaseMsgText 把一句文案的事实取成当前语言文本（页面槽位用）。
func purchaseMsgText(c *gin.Context, m facing.Msg) string { return response.MsgText(c, m) }

// purchaseTranslate 取当前请求语言的取词回调（与模板注入的 tr 同一个实现）。
func purchaseTranslate(c *gin.Context) func(key, fallback string) string {
	return shell.TranslateFor(c)
}

// —— 回跳筛选键 ——
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// shell.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
// 日志也干净，所以键表必须是同一份（不要在两处各写一遍字面量）。
var (
	purchaseSourceBackKeys = []string{"project", "type", "relatedParty", "status", "keyword", "page", "limit"}
	purchaseOrderBackKeys  = []string{"project", "status", "sourceId", "keyword", "page", "limit"}
)

// —— 写动作的出口 ——
//
// 形态与库存侧同一取舍：
//   · **成功**：{code:200,message,data}（response.Success）—— 不跳转、不给 URL。
//     导航由前端 data-write-nav 声明（无值 = 重载当前页），admin.js 在 htmx:afterRequest 照办。
//   · **失败**：4xx / 5xx + JSON。业务错误 400、其余 500，内部原文只进日志。

// purchaseFail 写动作失败：状态码由错误自己声明（pkg/facing.Statused），文案走同一条链。
//
// 分档口径见 pkg/response.ErrorStatus：声明处给过 404/409 就用它，业务错误默认 400，
// 其余 500。**不在这里判文案** —— 判据与文案必须是同一份事实（facing.FactOf）。
func purchaseFail(c *gin.Context, err error) {
	response.ErrorWithMessage(c, response.ErrorStatus(err), purchaseErrText(c, err))
}

// purchaseFailMsg 写动作失败（结论已经是事实，不是 error）：批量部分成功、未选中任何行、
// 批量 id 超限这类「参数与结果」都走这里，一律 400。
func purchaseFailMsg(c *gin.Context, msg facing.Msg) {
	response.ErrorMsg(c, http.StatusBadRequest, msg)
}

// purchaseBulkIDsMsg 批量 id 读取失败的一句结论（事实形态，失败出口用）。
//
// 判据是**类型事实**（*bulkids.IDsError）而不是文案比对：超限文案带两个只有错误类型知道的
// 数字（上限 Max 与**去重后**的本次条数 Count），拿 bulkids.MaxIDs 重算就丢了 Count。
func purchaseBulkIDsMsg(c *gin.Context, err error) facing.Msg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		if err != nil {
			logger.Scene(purchaseErrScene).
				With("path", c.Request.URL.Path).
				Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
		}
		return response.InternalMsg()
	}
	logger.Scene(purchaseErrScene).
		With("path", c.Request.URL.Path).
		With("count", tooMany.Count).
		With("max", tooMany.Max).
		Warn("批量操作被拒：提交的 id 数量超过上限")
	return facing.Msg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
}

// purchaseBulkSourcePartial 批量删除货源的部分成功结论（失败出口的事实形态）。
//
// 兜底一律写 `%s`：TranslateArgs 只接受 `%s` 位置占位符，词条里混进 `%d` 时它会丢弃词条
// 回落这里的兜底，而兜底若还是 `%d`、参数却是字符串，Sprintf 会产出
// `%!d(string=2)` 这种「看起来像结论」的垃圾。
var purchaseBulkSourcePartial = facing.Msg{
	Key:      purchaseenums.KeyBulkSourcePartial,
	Fallback: "已删除 %s 个，%s 个未能删除（仍被采购单或历史流水引用）",
}

// projectOptions 工程契约结果 → 页头下拉的选项（本包镜像，只带模板读的两项）。
//
// 为什么不直接交 projectcontract.ProjectResp：dto 不跨模块依赖，模板读的是
// purchasedto.ProjectOptionResp，这一层就是那次转换的家。
func projectOptions(in []projectcontract.ProjectResp) []purchasedto.ProjectOptionResp {
	out := make([]purchasedto.ProjectOptionResp, 0, len(in))
	for _, p := range in {
		out = append(out, purchasedto.ProjectOptionResp{ID: p.ID, Name: p.Name})
	}
	return out
}

// —— 列表分页（货源 / 采购入库两页共用）——
//
// 两页原先都是「handler 里写死上限 + 模板里没有分页条」：货源 200 条、采购单 100 条，
// 第 N+1 条起**静默消失**。这里接的是项目既有的分页设施（partials/pagination.html +
// pkg/pagination），与其它列表页共用同一套模板与样式。

// purchasePageParams 解析列表页分页参数（?page= / ?limit=）。
//
// limit 缺省用各页自己的一页条数，超过 maxSize 按 maxSize 封顶（与 service 的上限同口径）；
// 非数字 / 非正值一律回落 —— 后台页不因地址栏里一个脏参数而 500。
func purchasePageParams(rawPage, rawLimit, defaultSize, maxSize int) (page, limit int) {
	page = rawPage
	if page < 1 {
		page = 1
	}
	limit = rawLimit
	if limit < 1 {
		limit = defaultSize
	}
	if limit < 1 {
		limit = defaultSize
	}
	if limit > maxSize {
		limit = maxSize
	}
	return page, limit
}

// parseIntOr 解析正整数，空 / 脏 / 非正一律回落 fallback。
func parseIntOr(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	got, err := strconv.Atoi(raw)
	if err != nil || got < 1 {
		return fallback
	}
	return got
}

// clampPurchasePage 把页码收敛到实际总页数以内（total = 0 时收敛到第 1 页）。
//
// 必须在**取数之前**收敛：越界页码（手输 URL、过期书签、上一次筛选残留的 page）直接传给
// 列表接口时，服务端会老老实实返回一个空页，而分页条按收敛后的页码渲染 ——
// 「表格为空、分页条却显示第 2 页」这种自相矛盾的组合就是这样来的
// （pkg/pagination 只收敛分页条自己显示的那一页，不会回头改取数用的页码）。
func clampPurchasePage(page, limit int, total int64) int {
	if limit < 1 {
		limit = 1
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

// statusLabel 启停状态 → 当前语言展示文案（货源启用 / 停用）。
func statusLabel(tr func(key, fallback string) string, status string) string {
	if status == purchaseenums.StatusDisabled {
		return tr(purchaseenums.KeyStatusDisabled, "已停用")
	}
	return tr(purchaseenums.KeyStatusActive, "启用中")
}

// isHXRequest 这次请求是否由 htmx 发起（HX-Request: true）。
//
// 判据委托 reqparams.IsHXRequest（同一份实现）：两个出口必须认同同一个信号 ——
// 各写一份就会出现「某个模块的失败分档静默失效」。
func isHXRequest(c *gin.Context) bool {
	return reqparams.IsHXRequest(c)
}

// operatorFromContext 操作人标识：优先会话用户名（客户端传入被忽略），回落 user_id。
func operatorFromContext(c *gin.Context) (id string) {
	if name := strings.TrimSpace(builtin.GetUsername(c)); name != "" {
		return name
	}
	value, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case interface{ String() string }:
		return v.String()
	default:
		return ""
	}
}
