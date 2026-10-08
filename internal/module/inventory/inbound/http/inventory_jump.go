package inventoryhttp

// inventory_jump.go — 库存域后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302/303 + `?err=` / `?ok=` / `?done=` 回列表页：那条通道要求读侧再判一次
// 「这条提示是不是本仓给的」（inventoryPageErr / inventoryPageOk / inventoryPageDone /
// inventoryNoticeTexts / inventoryConclusion 就是那套），而查询参数不是可信边界。
// 文案改走响应体之后，读侧判定整批删除（见 inventory_page.go 的说明）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案必须**已过本模块白名单 / 已归口**（inventoryErrText / shell.BulkIDsFacingText /
//     inventoryBulkText 的产物）—— 原文只进日志，换个页面呈现不等于可以把 err.Error() 铺上去；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

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

func inventoryStockBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(inventoryenums.InventoryTitle, "库存管理")
}

func inventoryWarehouseBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(inventoryenums.InventoryWarehousesTitle, "仓库管理")
}

func inventoryReasonBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(inventoryenums.InventoryReasonsTitle, "变动原因字典")
}

func inventorySourceBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(inventoryenums.MsgInventorySourcesTitle, "货源管理")
}

func inventoryPurchaseBackText(c *gin.Context) string {
	return shell.TranslateFor(c)(inventoryenums.MsgInventoryPurchasesTitle, "采购入库")
}

// —— 提示页出口 ——

// inventoryJump 渲染整页提示：成功 1 秒后自动回列表，失败不自动跳（运营要看清楚原因）。
func inventoryJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// inventoryDoneText 单条写动作的成功回执（取当前语言，词条缺失回落中文）。
//
// 复用读侧时代就有的 admin.inventory.actionDone 词条（迁移 280）：改造只换传输通道，
// 不换「成功说的是哪句话」。
func inventoryDoneText(c *gin.Context) string {
	return shell.TranslateFor(c)(inventoryActionDoneKey, inventoryActionDoneFallback)
}
