package adminui

// page.go — UI 线与业务控制器的**唯一接线**：模板名与渲染实现由装配点注入。
//
// 为什么要有这一层：渲染是 UI 的事，业务模块不该持有它 —— 一旦 handler 里出现渲染调用，
// 就会顺带长出模板名、gin.H 装配、状态码分档、「失败页长什么样」这几样东西。
//
// 接线形态（业务模块只碰这两样）：

//	// *_router.go —— 装配点：模板名与路由/权限点同处，一眼能看全这个模块渲染了哪些屏
//	pages.GET("/products", perm, adminui.PageSlot(productListPageTemplate), handle.ProductsPage)

//	// *_handle.go —— 不 import adminui、不出现模板名与 c.HTML
//	func (h *ProductPageHandle) ProductsPage(c *gin.Context) {
//	    req, ok := binding.Query[productdto.ProductListReq](c)
//	    if !ok { return }
//	    data, err := h.reader.ProductListPage(c.Request.Context(), req)
//	    if err != nil { pageview.Fail(c, err); return }
//	    pageview.Render(c, data)
//	}
//
// 失败出口只有一条：**JSON**（{code,message,data} + 真实 HTTP 状态码），成功页才走 Jet。
// 页面里的失败态（读到这一屏、但这次没取到数）仍由视图给出 —— 与出口失败不是同一件事。

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/view"
)

// PageSlot 后台**整页**出口：页壳 + 取词 + 状态码 / no-store（实现见 RenderPage）。
func PageSlot(tpl string) gin.HandlerFunc {
	return view.Slot(tpl, RenderPage)
}

// BareSlot 后台**不带页壳**出口（htmx 局部更新片段）：只注入取词函数（实现见 RenderBare）。
func BareSlot(tpl string) gin.HandlerFunc {
	return view.Slot(tpl, RenderBare)
}
