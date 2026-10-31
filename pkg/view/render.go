package view

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

// Slot 为存量页面装配模板与渲染器；新页面由处理器调用 RenderTemplate 选择模板。
func Slot(tpl string, render Renderer) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ctxTplKey, tpl)
		c.Set(ctxRendererKey, render)
		c.Next()
	}
}

// UseRenderer 统一装配渲染实现；模板由页面处理器在 RenderTemplate 调用处声明。
func UseRenderer(render Renderer) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ctxRendererKey, render)
		c.Next()
	}
}

// RenderTemplate 直接选择模板，仍复用请求级上下文注入与统一失败出口。
func RenderTemplate(c *gin.Context, tpl string, data any) {
	c.Set(ctxTplKey, tpl)
	Render(c, data)
}

// Render 控制器交视图：成功页走 Jet，片段走片段出口。
//
// 视图为 nil 属编程错误（契约：视图恒非 nil）—— 按 500 处理，而不是让渲染器拿 nil 去
// 渲染出一张缺键的半截页（Jet 缺键会中断整页）。
func Render(c *gin.Context, data any) {
	render, tpl, ok := slotOf(c)
	if !ok {
		response.ErrorInternal(c, "view", errors.New("视图出口未装配：路由缺渲染器"))
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
//
// 日志场景默认从模板名推（admin/product/products.html → product），但**模块的既有 scene
// 可能与模板目录名不同**（例如 contenttemplate 模块的 scene 是 `content_template`，
// 而模板目录是 `admin/contenttemplate/`）。那种情况用 FailScene 显式给，
// 而不是让日志在两个名字下分家 —— 按 scene 捞日志是排障的入口，分家等于捞不全。
func Fail(c *gin.Context, err error) {
	_, tpl, _ := slotOf(c)
	FailScene(c, sceneOf(tpl), err)
}

// FailScene 与 Fail 相同，但显式指定日志场景（见 Fail 的说明）。
func FailScene(c *gin.Context, scene string, err error) {
	if errors.Is(err, response.ErrHandled) {
		return
	}
	// 状态码判据只有一份：pkg/response 的 ErrorStatus（声明过用声明的、
	// 业务错误 400、其余 500）。这里原先自己算了一遍 —— 同一套判据写两处，
	// 哪天 facing 的分档变了，只有一处会跟着改，另一处静默漂移。
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
