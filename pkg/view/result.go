package view

// result.go — 视图自带的渲染结论（HTTP 档位 + 缓存策略）。
//
// 为什么定义在这里（引擎门面）而不是渲染实现里：实现它的是业务 dto 的整页视图，
// 而 dto 不该为了一个接口去 import 渲染层 —— 那会把「业务模块 → UI 实现」的依赖固化下来，
// 每多一个页面就多一处反向引用。放在零 gin、零请求上下文的 pkg/view 里，
// 渲染实现引用它、视图实现它，两边都朝里。
//
// 典型实现（值接收者，零值即「200 且可缓存」）：
//
//	func (v ProductListPageResp) PageStatus() int   { return v.HTTPStatus }
//	func (v ProductListPageResp) PageNoStore() bool { return v.LoadFailed }
type Result interface {
	// PageStatus 这一屏的 HTTP 档位；返回 0 视为 200。
	PageStatus() int
	// PageNoStore 为 true 时渲染出口设 Cache-Control: no-store（失败页不能被缓存）。
	PageNoStore() bool
}
