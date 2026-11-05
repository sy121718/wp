package jet

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ScopeCarrier 表示「这个 ResponseWriter 带着本次请求的模板取词上下文」。
//
// 用接口而不是具体类型：渲染器只拿得到 http.ResponseWriter（gin render/render.go:10-15），
// 它需要一种不依赖 gin.Context、也不依赖全局变量的方式问出「这次是哪种语言」。
type ScopeCarrier interface {
	RenderScope() (RenderScope, bool)
}

// ScopeWriter 给当前请求的 gin.ResponseWriter 挂上模板取词上下文。
//
// 需要它的唯一原因：gin 的 render.Render 接口只拿到 http.ResponseWriter
// （gin render/render.go:10-15），jetInstance 想知道「这次请求是哪种语言」，
// 只能顺着这个 writer 拿。放在这里而不是全局回调，
// 是因为全局变量在进程内共享，语言必然串请求。
type ScopeWriter struct {
	gin.ResponseWriter
	scope RenderScope
}

// RenderScope 让 jetInstance 通过 ScopeCarrier 接口取到上下文。
func (w *ScopeWriter) RenderScope() (RenderScope, bool) {
	return w.scope, true
}

// Unwrap 让 httptest 之类的工具能穿透到原始 writer。
func (w *ScopeWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func WithRenderScope(c *gin.Context, scope RenderScope) {
	if c == nil || c.Writer == nil {
		return
	}
	c.Writer = &ScopeWriter{ResponseWriter: c.Writer, scope: scope}
}

// FailedRenderMiddleware 把模板渲染失败交给装配的协议出口。
//
// HTML 界面与数据 JSON 的具体出口由已声明的路由职责决定。
// 渲染器自身不写响应（见 jetInstance.renderError），只返回 *TemplateRenderError；
// 这里在 c.Next() 之后接住它，交给装配方传入的出口函数。
//
// 接管条件两条，缺一不可：
//   - c.Errors 里**存在**一条 *TemplateRenderError —— 注意不是「最后一条是」：
//     渲染失败之后链上可能还有人 c.Error(别的 err)，只看 Last 会把模板失败漏掉，
//     结果就是一个 500 的空白响应；
//   - 还没有写过响应体（Written() == false），否则重复写会破坏已经发出的 head。
//
// 多条模板错误时取最后一条（最贴近实际失败点），错误本身只用于判定与日志，
// 不拼进响应 —— 模板名与 Jet 报错原文都不上屏。
//
// 本中间件不改变「直接 c.HTML」与「view / adminui 渲染」两条路径中的任何一条：
// 两条路径最终都是 gin 的 c.Render → jetInstance.Render，失败形态一致。
func FailedRenderMiddleware(writeFailure func(c *gin.Context, err error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if c.Writer == nil || c.Writer.Written() {
			return
		}
		if writeFailure == nil {
			return
		}
		if err, ok := lastTemplateRenderError(c.Errors); ok {
			writeFailure(c, err)
		}
	}
}

// lastTemplateRenderError 从整条错误链里找最后一条模板渲染失败。
//
// 不看 Last：页面渲染失败后，后续代码可能再 c.Error(一个普通 err)，
// 那时 Last 是普通 err，但真正该接管的是前面那条模板错误。
func lastTemplateRenderError(errs []*gin.Error) (error, bool) {
	var found error
	var ok bool
	for _, e := range errs {
		if e == nil || e.Err == nil {
			continue
		}
		if _, isTemplate := AsTemplateRenderError(e.Err); isTemplate {
			found, ok = e.Err, true
		}
	}
	return found, ok
}
