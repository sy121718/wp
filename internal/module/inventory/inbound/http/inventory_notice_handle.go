package inventoryhttp

// inventory_jump.go — 库存域后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302/303 + `?err=` / `?ok=` / `?done=` 回列表页：那条通道要求读侧再判一次
// 「这条提示是不是本仓给的」（inventoryPageErr / inventoryPageOk / inventoryPageDone /
// inventoryNoticeTexts / inventoryConclusion 就是那套），而查询参数不是可信边界。
// 文案改走响应体之后，读侧判定整批删除（见 inventory_page.go 的说明）。
//
// 四条边界（同 shell.RenderJump 的注释）：
//   · **结论是事实、不是拼好的句子**：调用点给的是 adminui.Msg（key + 中文兜底 + `%s` 参数），
//     取词由模板注入的 tf 在**渲染期**完成（见 internal/adminui/prepare.go）—— 所以调用点
//     不需要知道当前是什么语言。本文件原先的 inventoryXxxBackText / inventoryDoneText 全是
//     Go 侧取词（`shell.TranslateFor(c)(key, 兜底)`），已收成下面的包级 adminui.Msg 变量；
//   · 文案必须**已过归口**（inventoryErrMsg / inventoryBulkIDsMsg / inventoryBulkXxx 的产物）
//     —— 原文只进日志，换个页面呈现不等于可以把 err.Error() 铺上去；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/internal/shell"
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

// —— 回跳链接文字（复用各页标题词条，不新增全站词条）——
//
// 包级变量而不是 `func(c *gin.Context) string`：这些文案**只由「key + 中文兜底」决定**，
// 与请求无关 —— 需要 gin.Context 的只有「取词」这一步，而取词属于渲染期（模板注入的 t）。
// 原先写成函数时，每一处调用点都在 Go 侧把词取好、再当成品串交给提示页，等于把「这段文案
// 属于哪个语言」在控制器里再审一遍。
var (
	inventoryStockBackMsg     = adminui.Msg{Key: inventoryenums.InventoryTitle, Fallback: "库存管理"}
	inventoryWarehouseBackMsg = adminui.Msg{Key: inventoryenums.InventoryWarehousesTitle, Fallback: "仓库管理"}
	inventoryReasonBackMsg    = adminui.Msg{Key: inventoryenums.InventoryReasonsTitle, Fallback: "变动原因字典"}
	inventorySourceBackMsg    = adminui.Msg{Key: inventoryenums.MsgInventorySourcesTitle, Fallback: "货源管理"}
	inventoryPurchaseBackMsg  = adminui.Msg{Key: inventoryenums.MsgInventoryPurchasesTitle, Fallback: "采购入库"}
)

// inventoryDoneMsg 单条写动作的成功回执。
//
// 复用读侧时代就有的 admin.inventory.actionDone 词条（迁移 280）：改造只换传输通道与
// 「谁取词」，不换「成功说的是哪句话」。
var inventoryDoneMsg = adminui.Msg{Key: inventoryActionDoneKey, Fallback: inventoryActionDoneFallback}

// inventoryMsgText 把一句结论取成当前语言文本（**片段槽位 / 页面提示条**用：那边的模板
// 直接渲染文本、不取词）。
//
// 这一类槽位（SubmitErr / Err / LoadError）本批还没取词化 —— 见 inventory_page.go
// 「错误文案」一节的说明；新槽位一律传 adminui.Msg 并在模板里用 tf 取词。
func inventoryMsgText(c *gin.Context, m adminui.Msg) string { return adminui.MsgText(c, m) }

// —— 提示页出口 ——

// inventoryJump 渲染整页提示：成功 1 秒后自动回列表，失败不自动跳（运营要看清楚原因）。
//
// 签名收的是事实（adminui.Msg）而不是成品串：正文与链接文字各一段 key + 兜底 + 参数，
// 填进 shell.Jump 后由提示页模板取词。
func inventoryJump(c *gin.Context, ok bool, msg adminui.Msg, back string, backText adminui.Msg) {
	jump := shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
	}
	if ok {
		jump.Seconds = 1
	}
	shell.RenderJump(c, jump)
}
