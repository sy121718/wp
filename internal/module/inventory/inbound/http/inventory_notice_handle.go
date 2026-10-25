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
	"net/http"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	projectcontract "go_wp/internal/module/project/contract"
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
	// 仓库商品 SKU 台账页的筛选键（迁移 621）。
	inventorySKUBackKeys = []string{"project", "warehouseId", "keyword", "claimed", "page", "limit"}
	// 调拨页的筛选键（迁移 627，W3）。
	inventoryTransferBackKeys = []string{"project", "status", "keyword", "page", "limit"}
)

// inventoryMsgText 把一句文案的事实取成当前语言文本（页面槽位 / 片段槽位用）。
func inventoryMsgText(c *gin.Context, m facing.Msg) string { return response.MsgText(c, m) }

// —— 写动作的出口 ——
//
// 两种形态，都由本文件给出，控制器里没有第三种：
//
//   · **成功**：{code:200,message,data}（response.Success）—— **不跳转、不给 URL**。
//     导航是前端的事：写表单在模板里用 data-write-nav 声明结果（无值 = 重载当前页），
//     admin.js 在 htmx:afterRequest 里照办。服务端算「回哪个 URL」等于把筛选与页码
//     再实现一遍 —— 而浏览器地址栏里本来就有那一份，且它才是用户眼里的当前状态。
//   · **失败**：4xx / 5xx + JSON（{code,message,data}）。业务错误 400、其余 500，
//     内部原文只进日志、对外只给归口文案。
//
// 为什么把结论塞进响应体而不是渲染一页：提示页是「又一个要维护的模板 + 一套要传的事实」
// （OK / Msg / MsgArgs / Back / BackText 五个字段，每个写动作各填一遍），而它承载的信息
// 与「回列表」完全重合。失败也不需要它 —— htmx 对 4xx 不 swap，admin.js 弹 toast，
// 抽屉与输入原样保留。

// inventoryFail 写动作失败：状态码由错误自己声明（pkg/facing.Statused），文案走同一条链。
//
// 分档口径见 pkg/response.ErrorStatus：声明处给过 404/409 就用它，业务错误默认 400，
// 其余 500。**不在这里判文案** —— 判据与文案必须是同一份事实（facing.FactOf），
// 分成两处就会出现「中文原因配 500」「找不到配 400」这类只在出事时才发现的偏差。
func inventoryFail(c *gin.Context, err error) {
	response.ErrorWithMessage(c, response.ErrorStatus(err), inventoryErrText(c, err))
}

// inventoryFailMsg 写动作失败（结论已经是事实，不是 error）：批量部分成功、未选中任何行、
// 批量 id 超限这类「参数与结果」都走这里，一律 400。
func inventoryFailMsg(c *gin.Context, msg facing.Msg) {
	response.ErrorMsg(c, http.StatusBadRequest, msg)
}

// projectOptions 工程契约结果 → 页头下拉的选项（本包镜像，只带模板读的两项）。
//
// 为什么不直接交 projectcontract.ProjectResp：dto 的视图字段是
// inventorydto.ProjectOptionResp（dto 不跨模块依赖），这一层就是那次转换的家。
func projectOptions(in []projectcontract.ProjectResp) []inventorydto.ProjectOptionResp {
	out := make([]inventorydto.ProjectOptionResp, 0, len(in))
	for _, p := range in {
		out = append(out, inventorydto.ProjectOptionResp{ID: p.ID, Name: p.Name})
	}
	return out
}
