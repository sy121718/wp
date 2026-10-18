package adminui

// page.go — UI 线的**唯一接线**：控制器交「视图 + 错误」，渲染在这里发生。
//
// 为什么要有这一层（而不是让每个 handler 末尾写 RenderPage / c.HTML）：
// 渲染是 UI 的事，业务模块不该持有它 —— 一旦 handler 里出现渲染调用，就会顺带长出
// 模板名、gin.H 装配、状态码分档、「失败页长什么样」这几样东西，于是每多一个页面
// 就多一份渲染实现。实测态：18 个模块里 159 处 c.HTML、136 处 Prepare，product 自己
// 13 处 RenderPage —— 同一个后台有四套页面出口。
//
// 接线形态（业务模块只碰这两行）：
//
//	// *_router.go —— 装配点：模板名与路由/权限点同处，一眼能看全这个模块渲染了哪些屏
//	pages.GET("/products", adminui.PageAuthz("/api/product/list"),
//	    adminui.Page(productListPageTemplate, handle.ProductsPage))
//
//	// *_handle.go —— 不再 import adminui、不再出现模板名与 c.HTML
//	func (h *ProductPageHandle) ProductsPage(c *gin.Context) (any, error) {
//	    req, ok := binding.Query[productdto.ProductListReq](c)
//	    if !ok { return nil, response.ErrHandled }
//	    return h.reader.ProductListPage(c.Request.Context(), req)
//	}
//
// 失败出口只有一条：**JSON**（`{code,message,data}` + 真实 HTTP 状态码），
// 成功页才走 Jet。页面里的失败态（LocadFailed / LoadError）是「读到了、这次没读到」，
// 仍由视图给出，与这里说的出口失败（参数不合法 / 取数抛错）不是同一件事。

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

// ViewHandler 页面控制器的形状：交一份视图（dto 结构体 / gin.H），或一个错误。
//
// 视图为 nil 且 err 为 nil 属编程错误（契约上视图恒非 nil）—— 由出口按 500 处理，
// 而不是让 c.HTML 拿 nil 去渲染出一张缺键的半截页。
type ViewHandler func(c *gin.Context) (any, error)

// Page 后台**整页**出口：页壳 + 取词 + 状态码 / no-store（见 RenderPage）。
func Page(tpl string, h ViewHandler) gin.HandlerFunc {
	return wrapView(tpl, h, RenderPage)
}

// Bare 后台**不带页壳**出口（htmx 局部更新片段）：只注入取词函数，一律 200（见 RenderBare）。
func Bare(tpl string, h ViewHandler) gin.HandlerFunc {
	return wrapView(tpl, h, RenderBare)
}

// wrapView 两条出口共用的接线：跑 handler → 失败转 JSON → 成功交给渲染实现。
func wrapView(tpl string, h ViewHandler, render func(*gin.Context, string, any)) gin.HandlerFunc {
	return func(c *gin.Context) {
		view, err := h(c)
		if err != nil {
			if errors.Is(err, response.ErrHandled) {
				// handler 自己写完了（SSE / 下载 / 302 / 空状态码）：不要再补一个 JSON。
				return
			}
			failView(c, tpl, err)
			return
		}
		if view == nil {
			failView(c, tpl, errors.New("页面视图为空（契约：视图恒非 nil）"))
			return
		}
		render(c, tpl, view)
	}
}

// failView 页面出口失败的唯一形态：JSON + 真实状态码。
//
// 业务错误（pkg/facing 判据）→ 400；其余（驱动 / 网络 / 编程错误）→ 500 + 归口文案，
// 原文只进结构化日志。htmp 的 4xx/5xx 不 swap，前端因此由 admin.js 统一接管
// htmx:responseError 显示 message（不接管就是「点了没反应」）。
func failView(c *gin.Context, tpl string, err error) {
	// 档位在**声明处**：模块 enums 给 key 挂 404/409（pkg/facing.WithStatus），
	// 未登记的按业务错误 400、其余 500。出口不再按接口猜（那会让同一个 err 换个接口就换含义）。
	status := http.StatusInternalServerError
	if declared, ok := facing.StatusOf(err); ok {
		status = declared
	} else if response.IsBusinessError(err) {
		status = http.StatusBadRequest
	}
	response.ErrorAuto(c, status, sceneOf(tpl), err)
}

// sceneOf 模板名 → 日志场景：`admin/product/products.html` → `product`。
//
// 场景只进结构化日志（谁在哪儿失败），不参与响应；从模板名推是为了让接线少一个参数 ——
// 模板名的第一段就是归属，另传一个 scene 迟早与它对不上。
func sceneOf(tpl string) string {
	parts := strings.Split(tpl, "/")
	if len(parts) >= 2 && parts[0] == "admin" {
		return parts[1]
	}
	return parts[0]
}
