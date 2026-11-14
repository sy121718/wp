package routers

import (
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/view"
)

// errInstanceNotReady 是闸门拒绝业务流量的错误事实：503 + 归口文案。
var errInstanceNotReady = facing.WithStatus(
	facing.NewMessage("uiNotReady", "实例未就绪，请稍后重试"), http.StatusServiceUnavailable)

// startupGate 是「装配未通过前拒绝接业务流量」的进程内闸门。
//
// 权限目录与路由声明不一致时，只让 /readyz 报 not_ready 并不能阻止流量：
// 进程照样监听端口、业务路由照样可访问，等于把不一致的授权索引暴露给真实请求。
// 闸门在全部路由注册之前装上，装配收尾时按核对结果一次性落定；落定后不再变化，
// 不实现运行中重新打开。
type startupGate struct {
	// state: 0 = 等待装配（默认拒绝），1 = 通过，2 = 拒绝
	state atomic.Int32
}

// settle 由装配收尾调用：ok 为 true 才放行业务流量。
func (g *startupGate) settle(ok bool) {
	if ok {
		g.state.Store(1)
		return
	}
	g.state.Store(2)
}

// allow 报告当前是否放行；等待装配状态按拒绝处理（fail closed）。
func (g *startupGate) allow() bool { return g.state.Load() == 1 }

// middleware 拒绝未通过装配的实例上的业务请求；探针端点始终放行，
// 否则外部无法读取 not_ready 的原因。
func (g *startupGate) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if g.allow() || probePath(c.Request.URL.Path) {
			c.Next()
			return
		}
		// 503 也遵守路由已声明的协议：交共享失败出口，由它按路由声明选择
		// HTML 错误区或 JSON。不能对浏览器页面直接回一段 JSON。
		c.Header("Cache-Control", "no-store")
		view.FailStatus(c, http.StatusServiceUnavailable, "startup", errInstanceNotReady)
		c.Abort()
	}
}

// probePath 是运维探针：它们只报告状态，不属于业务流量。
func probePath(path string) bool {
	return path == "/livez" || path == "/readyz"
}
