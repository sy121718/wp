package view

// render.go — 页面出口的**唯一接线**：控制器只写 pageview.Render(c, data) / pageview.Fail(c, err)。
//
// 模板名与渲染实现由路由装配注入（adminui.PageSlot / BareSlot）—— 控制器因此不持模板名、
// 不 import 渲染层，也不会退化成「每屏一份 RenderPage 调用」。渲染仍然是 UI 线的实现
// （internal/adminui 的 RenderPage / RenderBare），这里只做两件事：
//
//  1. 把「这一屏渲染哪个模板、谁来渲染」从装配点带到 handler（请求上下文，一个请求一次）；
//  2. 失败的唯一形态是 JSON（{code,message,data} + 真实状态码），不吞不转。
//
// 为什么在 pkg 而不是 internal：控制器（业务模块）要调它，而依赖方向是
// cmd → routers → module → pkg；放进 internal 会让业务模块反向依赖 UI 实现。

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/response"
)

// Renderer 渲染实现：由 UI 线提供（整页 = 页壳 + 取词；片段 = 只注入取词）。
type Renderer func(c *gin.Context, tpl string, data any)

const (
	ctxTplKey      = "view.template"
	ctxRendererKey = "view.renderer"
)

// Slot 装配用中间件：把这一屏的模板名与渲染实现放进请求上下文，随后交给 handler。
//
// 用法（路由装配点，模板名与权限点同处）：
//
//	pages.GET("/products", perm, adminui.PageSlot(ProductListPageTemplate), h.ProductsPage)
func Slot(tpl string, render Renderer) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ctxTplKey, tpl)
		c.Set(ctxRendererKey, render)
		c.Next()
	}
}

// Render 控制器交视图：成功页走 Jet，片段走片段出口。
//
// 视图为 nil 属编程错误（契约：视图恒非 nil）—— 按 500 处理，而不是让渲染器拿 nil 去
// 渲染出一张缺键的半截页（Jet 缺键会中断整页）。
func Render(c *gin.Context, data any) {
	render, tpl, ok := slotOf(c)
	if !ok {
		response.ErrorInternal(c, "view", errors.New("视图出口未装配：路由缺 adminui.PageSlot / BareSlot"))
		return
	}
	if data == nil {
		response.ErrorInternal(c, sceneOf(tpl), errors.New("页面视图为空（契约：视图恒非 nil）"))
		return
	}
	render(c, tpl, data)
}

// Fail 控制器交失败：与 JSON 接口同一个形状（业务错误 400、声明过状态码的按声明、其余 500）。
//
// response.ErrHandled 表示 handler 自己已经写完响应（SSE / 下载 / 原始字节）：不补第二个响应。
func Fail(c *gin.Context, err error) {
	if errors.Is(err, response.ErrHandled) {
		return
	}
	// 状态码判据只有一份：pkg/response 的 ErrorStatus（声明过用声明的、
	// 业务错误 400、其余 500）。这里原先自己算了一遍 —— 同一套判据写两处，
	// 哪天 facing 的分档变了，只有一处会跟着改，另一处静默漂移。
	_, tpl, _ := slotOf(c)
	scene := sceneOf(tpl)
	response.ErrorAuto(c, response.ErrorStatus(err), scene, err)
}

// sceneOf 模板名 → 日志场景：admin/product/products.html → product。
//
// 场景只进结构化日志（谁在哪儿失败），不参与响应；从模板名推是为了少一个参数 ——
// 另传一个 scene 迟早与模板名对不上。未装配时给 view。
func sceneOf(tpl string) string {
	parts := strings.Split(tpl, "/")
	if len(parts) >= 2 && parts[0] == "admin" {
		return parts[1]
	}
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "view"
}

func slotOf(c *gin.Context) (Renderer, string, bool) {
	raw, ok := c.Get(ctxRendererKey)
	if !ok {
		return nil, "", false
	}
	render, ok := raw.(Renderer)
	if !ok || render == nil {
		return nil, "", false
	}
	tpl, _ := c.Get(ctxTplKey)
	name, _ := tpl.(string)
	return render, name, true
}
