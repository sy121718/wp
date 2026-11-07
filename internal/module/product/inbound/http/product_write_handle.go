package producthttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	pageview "go_wp/pkg/view"
)

// runProductWrite 是商品写端点的统一骨架：绑定 → 调用服务 → 统一成功出口。
//
// 三个回调各自只做一件事；绑定或调用失败已写出响应，控制器不再重复判断。
func runProductWrite[T any](c *gin.Context,
	call func(context.Context, *T) (any, error),
	success func(*gin.Context)) {
	c.Header("Cache-Control", "no-store")
	var req T
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := call(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	success(c)
}

// runProductWriteErr 用于只返回 error 的 service 方法（删除一类）。
func runProductWriteErr[T any](c *gin.Context,
	call func(context.Context, *T) error,
	success func(*gin.Context)) {
	c.Header("Cache-Control", "no-store")
	var req T
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if err := call(c.Request.Context(), &req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	success(c)
}

// bindProductForm 绑定商品写表单；失败已由这里写出 400。
func bindProductForm[T any](c *gin.Context) *T {
	c.Header("Cache-Control", "no-store")
	var req T
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return nil
	}
	return &req
}

// runProductWriteResult 用于「绑定 → 注入可信字段 → 调用 → 渲染结果片段」的端点。
func runProductWriteResult[T any](c *gin.Context,
	prepare func(*gin.Context, *T),
	call func(context.Context, *T) (any, error),
	render func(*gin.Context, any)) {
	req := bindProductForm[T](c)
	if req == nil {
		return
	}
	if prepare != nil {
		prepare(c, req)
	}
	res, err := call(c.Request.Context(), req)
	if err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	render(c, res)
}

// runProductWriteErrWith 用于「绑定 → 注入可信字段 → 调用（只回 error）→ 成功出口」。
func runProductWriteErrWith[T any](c *gin.Context,
	prepare func(*gin.Context, *T),
	call func(context.Context, *T) error,
	success func(*gin.Context)) {
	req := bindProductForm[T](c)
	if req == nil {
		return
	}
	if prepare != nil {
		prepare(c, req)
	}
	if err := call(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	success(c)
}

// runProductWriteEnd 用于「绑定 → 调用（只回 error，无结果）→ 成功出口」，
// 且 service 方法签名已与表单类型一致、不需要额外字段注入的端点。
func runProductWriteEnd[T any](c *gin.Context, call func(context.Context, *T) error, success func(*gin.Context)) {
	req := bindProductForm[T](c)
	if req == nil {
		return
	}
	if err := call(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	success(c)
}

// productWriteOK 是纯成功的公共出口。
func productWriteOK(c *gin.Context) { pageview.Success(c) }
