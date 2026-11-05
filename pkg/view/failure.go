package view

import (
	"errors"
	"net/http"
	"reflect"
	"runtime"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

// FailureWriter 将失败交给已声明协议的出口；模板与取词由装配方负责。
type FailureWriter func(c *gin.Context, status int, scene string, err error)

const (
	ctxFailureWriter  = "view.failure.writer"
	ctxFragment       = "view.failure.fragment"
	ctxFailureWritten = "view.failure.written"
)

// HTMLPage 在路由注册处声明完整 HTML 页面，不根据请求头切换协议。
func HTMLPage(c *gin.Context) { c.Next() }

// HTMLFragment 在路由注册处声明局部 HTML，失败响应不携带整页壳。
func HTMLFragment(c *gin.Context) { c.Next() }

var (
	htmlPageHandler     = runtime.FuncForPC(reflect.ValueOf(HTMLPage).Pointer()).Name()
	htmlFragmentHandler = runtime.FuncForPC(reflect.ValueOf(HTMLFragment).Pointer()).Name()
)

// FailureMiddleware 必须装在可能提前拒绝的中间件之前。
// Gin 匹配路由后才执行中间件，HandlerNames 包含尚未执行的完整处理链，
// 因而可识别明确注册的标记，无需全局路由表、路径前缀或客户端协商。
func FailureMiddleware(write FailureWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, name := range c.HandlerNames() {
			if name == htmlPageHandler || name == htmlFragmentHandler {
				c.Set(ctxFailureWriter, write)
				c.Set(ctxFragment, name == htmlFragmentHandler)
				break
			}
		}
		c.Next()
	}
}

// IsFragment 返回路由声明的局部响应类型，不读取 HX-Request 或 Accept。
func IsFragment(c *gin.Context) bool { return c.GetBool(ctxFragment) }

// FailStatus 保留显式 HTTP 状态，按路由职责选择 HTML 或数据 JSON 失败出口。
func FailStatus(c *gin.Context, status int, scene string, err error) {
	if errors.Is(err, response.ErrHandled) || c.Writer.Written() || c.GetBool(ctxFailureWritten) {
		return
	}
	c.Set(ctxFailureWritten, true)
	// 原生 binder 可包装 MaxBytesError；在出口统一转为安全事实，不暴露解析上下文。
	var bodyLimit *http.MaxBytesError
	if errors.As(err, &bodyLimit) {
		status = http.StatusRequestEntityTooLarge
		err = facing.New(enums.ErrRequestEntityTooLarge)
	}
	if raw, ok := c.Get(ctxFailureWriter); ok {
		if write, ok := raw.(FailureWriter); ok && write != nil {
			write(c, status, scene, err)
			return
		}
	}
	response.ErrorAuto(c, status, scene, err)
}
