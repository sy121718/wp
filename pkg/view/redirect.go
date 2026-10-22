package view

// redirect.go — 控制器的**跳转出口**：写后跳转一律 303，页面跳转用 302。
//
// 为什么需要这个助手，而不是各处写 `c.Redirect(http.StatusSeeOther, target)`：
//
//	状态码的语义差别不是风格问题。303（See Other）明确告诉客户端「用 GET 去取那个地址」，
//	而 302（Found）在**部分客户端**会被当成「重放原方法」—— 于是「提交一次」变成
//	「提交两次」，表单页重复下单/重复建记录就是这么来的（这条是 block_page.go 与
//	ai_page.go 两处注释各自记下的实测结论，不是理论）。
//
//	问题在于「写后该用 303」这条知识只存在于那两处注释里。全仓 27 处 StatusFound、
//	4 处 StatusSeeOther，靠人记住「哪个是写后」——记不住的时候就写 302，而症状
//	（偶尔重复提交）几乎不可能被归因到状态码。所以判断收进助手：
//
//	AfterWrite  写动作之后的 PRG  —— 303
//	GoTo        页面跳转（读动作）—— 302
//
// 两个名字都是**动作意图**而不是状态码数字：调用点写「我是写后跳转」，
// 不再写「我用 303」。将来若发现某类客户端要换码，只改这里。
//
// 为什么不做一个 `Redirect(c, target)` 自动判断「刚才是不是写动作」：做不到。
// 助手看不到调用它的方法是不是 POST handler（gin 的 Method 在中间件链路里可被改写，
// HX-Request 分支也不看 method），而猜错的代价正好是这个助手要消灭的那个缺陷。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/backpath"
)

// AfterWrite 写动作成功后跳转（PRG 的 P 部分）：303。
//
// 用于 POST/PUT/PATCH/DELETE 处理完、要让浏览器用 GET 去看结果的地方。
// 302 在部分客户端会被当成「重放原方法」，重复提交就是这么来的。
//
// htmx 请求不走这里：htmx 认 HX-Redirect 响应头（见 HXRedirect），
// 对 3xx 它自己不做整页跳转。
func AfterWrite(c *gin.Context, target string) {
	if c == nil {
		return
	}
	if target = strings.TrimSpace(target); target == "" {
		return
	}
	c.Redirect(http.StatusSeeOther, target)
}

// GoTo 页面跳转（读动作）：302。
//
// 用于「这一屏不该在这里看，去那一屏」——语言切换、登录后回跳、
// 新建成功后把人带到详情页。**写后跳转用 AfterWrite**，两者别混。
func GoTo(c *gin.Context, target string) {
	if c == nil {
		return
	}
	if target = strings.TrimSpace(target); target == "" {
		return
	}
	c.Redirect(http.StatusFound, target)
}

// Back 跳回调用点声明的路径，并按 keys 从当前请求串透传参数：303。
//
// 与 backpath.Build 的分工：那个包负责**构造**地址（白名单键、长度上限、控制字符），
// 本函数负责**发出去**（状态码 + 哪来的参数）。拆开是既有架构决定 ——
// backpath 的包注释明写「不 import gin、不读 *gin.Context」，为的是能被任何层复用。
//
// keys 是**调用点显式列出的白名单**（不是「原样回带全部参数」）：
// 原样回带会把 `page=999`、`_=` 之类一起带回去，而且「哪些参数值得保留」本来就是
// 调用点的知识。透传值超过 backpath.MaxQueryValueBytes 或含控制字符时被丢弃
// （判据在 backpath，这里不重复实现）。
//
// path 不是站内相对路径时**不跳转**（返回 false），由调用点决定兜底落点：
// backpath 不替调用方选落点（`/admin` 是控制面概念），本函数也不替。
func Back(c *gin.Context, path string, keys ...string) bool {
	if c == nil || c.Request == nil {
		return false
	}
	target, ok := backpath.Build(path, c.Request.URL.Query(), keys...)
	if !ok {
		return false
	}
	c.Redirect(http.StatusSeeOther, target)
	return true
}

// HXRedirect 让 htmx 做整页跳转（HX-Redirect 响应头 + 200）。
//
// 为什么 htmx 不能直接吃 3xx：htmx 2.0.4 对重定向是**跟随 XHR**，不会改浏览器地址栏，
// 而 HX-Redirect 才是「整页换掉」。实测口径见 internal/templates/CLAUDE.md。
//
// 与 AfterWrite 的关系：同一个写动作，浏览器表单走 AfterWrite（303），
// htmx 发起的走这里。调用点按 IsHXRequest 分流（见 ai_page.go 的 redirectWhere 先例）。
func HXRedirect(c *gin.Context, target string) {
	if c == nil {
		return
	}
	if target = strings.TrimSpace(target); target == "" {
		return
	}
	c.Header("HX-Redirect", target)
	c.Status(http.StatusOK)
}
