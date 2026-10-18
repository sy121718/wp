package inventoryhttp

// inventory_notice_handle.go — 库存域后台页写动作的出口（成功回列表、失败 JSON）。
//
// 两个出口函数在文件末尾：inventoryDone（成功）/ inventoryFail、inventoryFailMsg（失败）。
// 本文件另一半是**回跳地址**：写表单的 action 上带着当前筛选，写完跳回同一批筛选。
// 键表与渲染时的拼串用同一份（inventoryQueryFromRequest），两处分叉的表现是「写完跳回去
// 筛选静默丢了」—— 页面不报错、日志也干净。
//
// 结论的载体几经变化：先是 302 + ?err= / ?ok=（查询参数不是可信边界，废弃），
// 然后是整页提示页（又一个要维护的模板 + 五个要填的事实字段），现在是响应体：
// 成功回列表、失败 {code,message,data}。
import (
	"net/url"
	"strings"

	"net/http"

	"github.com/gin-gonic/gin"
	"go_wp/internal/text"

	"go_wp/internal/shell"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

// 各页回跳筛选键。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// shell.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
// 日志也干净，所以键表必须是同一份（不要在两处各写一遍字面量）。
var (
	inventoryStockBackKeys     = []string{"project", "sku", "variantId", "warehouseId", "direction", "reasonCode", "timeFrom", "timeTo", "page", "limit"}
	inventoryWarehouseBackKeys = []string{"project"}
	inventoryReasonBackKeys    = []string{"project"}
	inventorySourceBackKeys    = []string{"project", "type", "relatedParty", "status", "keyword", "page", "limit"}
	inventoryPurchaseBackKeys  = []string{"project", "status", "sourceId", "keyword", "page", "limit"}
)

// inventoryQueryFromRequest 本次请求 query 里的列表上下文（表单 action 上带回来的那一段）。
//
// 同一份实现同时服务「渲染时拼进表单 action」与「失败重渲片段时再拼一次」：
// 两者产出的形状必须一致，否则会出现「首屏表单 action 带 A、失败重渲后带 B」。
// 空值丢弃、编码走 url.Values（键有序、产物稳定）。
func inventoryQueryFromRequest(c *gin.Context, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	in := c.Request.URL.Query()
	out := url.Values{}
	for _, k := range keys {
		if v := strings.TrimSpace(in.Get(k)); v != "" {
			out.Set(k, v)
		}
	}
	return out.Encode()
}

// —— 回跳地址（每页一个，键表与上面同一份）——

func inventoryStockBack(c *gin.Context) string {
	return shell.BackPath(c, inventoryPagePath, inventoryStockBackKeys...)
}

func inventoryWarehouseBack(c *gin.Context) string {
	return shell.BackPath(c, inventoryWarehousesPath, inventoryWarehouseBackKeys...)
}

func inventoryReasonBack(c *gin.Context) string {
	return shell.BackPath(c, inventoryReasonsPath, inventoryReasonBackKeys...)
}

func inventorySourceBack(c *gin.Context) string {
	return shell.BackPath(c, inventorySourcesPath, inventorySourceBackKeys...)
}

func inventoryPurchaseBack(c *gin.Context) string {
	return shell.BackPath(c, inventoryPurchasesPath, inventoryPurchaseBackKeys...)
}

// inventoryMsgText 把一句文案的事实取成当前语言文本（页面槽位 / 片段槽位用）。
func inventoryMsgText(c *gin.Context, m facing.Msg) string { return text.MsgText(c, m) }

// —— 写动作的出口 ——
//
// 两种形态，都由本文件给出，控制器里没有第三种：
//
//   · **成功**：回到列表（redirectWhere）—— htmx 走 HX-Redirect、原生走 302。
//     列表本身已经变了，这就是回执；不再有「成功提示页」那一跳。
//   · **失败**：4xx / 5xx + JSON（{code,message,data}）。业务错误 400、其余 500，
//     内部原文只进日志、对外只给归口文案。
//
// 为什么把结论塞进响应体而不是渲染一页：提示页是「又一个要维护的模板 + 一套要传的事实」
// （OK / Msg / MsgArgs / Back / BackText 五个字段，每个写动作各填一遍），而它承载的信息
// 与「回列表」完全重合。失败也不需要它 —— htmx 对 4xx 不 swap，admin.js 弹 toast，
// 抽屉与输入原样保留。

// inventoryDone 写动作成功：回列表（不渲染任何页面）。
func inventoryDone(c *gin.Context, back string) {
	redirectWhere(c, back)
}

// inventoryFail 写动作失败（按 error 判档）：业务错误 400、其余 500。
//
// 状态码与文案必须用**同一份判据**：这里判的是 facing.FactOf（与 inventoryErrMsg 相同），
// 不是 response.IsBusinessError —— 后者的正则整串锚定，认不出模块用 fmt.Errorf("%w: 明细")
// 拼出来的复合错误（ErrExternalSKUProductConflict: …），会把一条业务拒绝判成 500，
// 而文案那一侧却已经把它渲染成了中文原因。判据分叉的症状正是「400 的句子配 500 的状态码」。
func inventoryFail(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	if fact, ok := facing.FactOf(err); ok && fact.Business {
		status = http.StatusBadRequest
	}
	response.ErrorWithMessage(c, status, inventoryErrText(c, err))
}

// inventoryFailMsg 写动作失败（结论已经是事实，不是 error）：批量部分成功、未选中任何行、
// 批量 id 超限这类「参数与结果」都走这里，一律 400。
func inventoryFailMsg(c *gin.Context, msg facing.Msg) {
	response.ErrorWithMessage(c, http.StatusBadRequest, text.MsgText(c, msg))
}
