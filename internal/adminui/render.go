package adminui

// render.go — 后台的**渲染出口**（整页与不带页壳两条）：控制器最后一步只该是一行渲染。
//
// 为什么需要它：控制器的终局是「绑定并校验请求 → 调 service → 渲染」四步（见
// internal/module/CLAUDE.md §inbound/http）。渲染这一步如果每个 handler 自己写，就会重复三件事：
//   · HTTP 分档（200 / 400 参数不合法 / 500 取数失败）；
//   · no-store（失败页不该被缓存，否则重试看到的是缓存的失败页）；
//   · 取词函数注入（PrepareView / PrepareBareView，漏了是**静默**空串）。
// 这三件事的判据都属于「这一屏的结论」，所以由**视图**给出（实现 PageResult），渲染出口只读它 ——
// handler 因此不再需要 `if errors.Is(...) { status = 400 }` 这类分档代码。
//
// 失败页与成功页渲染**同一份模板**：差别只有状态码 / no-store / 视图里的失败槽。两份模板必然漂移，
// 而漂移的表现是「失败页缺了某个必需键」→ 渲染中断 → 500（见 internal/templates/CLAUDE.md）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/view"
)

// PageResult 视图自带的渲染结论（HTTP 分档 + 缓存策略）。
//
// 形状定义在 pkg/view.Result —— 实现它的是业务 dto 的整页视图，dto 不该 import 渲染层。
// 本包只留这个别名给存量调用点（渲染出口、注释与测试），**新代码用 view.Result**。
//
// 由 dto 视图实现（值接收者，零值即「200 且可缓存」）：
//
//	func (v ProductNewPageResp) PageStatus() int    { return v.HTTPStatus }
//	func (v ProductNewPageResp) PageNoStore() bool  { return v.LoadFailed }
//
// 不实现该接口的视图按「200 + 可缓存」处理。
type PageResult = view.Result

// RenderPage 后台整页渲染出口：视图 → 状态码 / 缓存头 / 取词注入 → c.HTML。
func RenderPage(c *gin.Context, tpl string, view any) {
	status := http.StatusOK
	if r, ok := view.(PageResult); ok {
		if s := r.PageStatus(); s > 0 {
			status = s
		}
		if r.PageNoStore() {
			c.Header("Cache-Control", "no-store")
		}
	}
	c.HTML(status, tpl, PrepareView(c, view))
}

// RenderBare 后台**不带页壳**的渲染出口（htmx 局部更新）：只注入取词函数，页壳那套键不给。
//
// 它与 RenderPage 不是两条渲染路径：同一份模板、同一份视图，区别只在套不套页壳 ——
// 局部更新换掉页面里的一块，套上页壳就是把整页塞进那一块里。
//
// 这条出口**一律 200**：htmx 2.0.4 把 5xx 判成 `swap:false`，失败返回 500 等于「用户点了没反应」。
// 失败因此表达在视图的失败槽里（同 internal/templates/CLAUDE.md 的实测记录）。
func RenderBare(c *gin.Context, tpl string, view any) {
	c.HTML(http.StatusOK, tpl, PrepareBareView(c, view))
}

// PrepareBareView 不带页壳版的视图渲染入口：ViewMap + PrepareBare。
func PrepareBareView(c *gin.Context, view any) gin.H {
	return PrepareBare(c, ViewMap(view))
}
