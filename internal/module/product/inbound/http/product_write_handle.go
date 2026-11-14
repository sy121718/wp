package producthttp

import (
	"context"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
)

// runProductWrite 是商品写端点的统一骨架：绑定 → 调用服务 → 统一成功出口。
//
// 三个回调各自只做一件事；绑定或调用失败已写出响应，控制器不再重复判断。
func runProductWrite[T any](c *gin.Context,
	call func(context.Context, *T) (any, error),
	success func(*gin.Context)) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[T](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := call(c.Request.Context(), req); err != nil {
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
	req, err := binding.ParseForm[T](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if err := call(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	success(c)
}

// formValidator 是表单自己的业务校验入口。
//
// Gin 不会自动调用任意 Validate()，需要绑定后显式调用 —— 否则「把校验搬进 DTO」
// 只是换了个位置，运行时照样不生效。
type formValidator interface {
	Validate() error
}

// formPresenceValidator 在绑定前检查原始表单的「出现 / 清空」语义：
// 绑定会把空串压成零值，绑定后就分不清「显式 0」与「空白」。
type formPresenceValidator interface {
	ValidatePresence(url.Values) error
}

// bindProductForm 绑定商品写表单并跑表单级校验；失败已由这里写出 400。
func bindProductForm[T any](c *gin.Context) *T {
	c.Header("Cache-Control", "no-store")
	var req T
	if err := bindProductFormInto(c, &req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return nil
	}
	return &req
}

// bindProductFormInto 绑定一张表单并跑「出现/清空」与「业务」两级校验。
//
// 绑定前先解析原始表单：PostForm 在 ParseForm 之前是空 map，
// 直接读会把「字段缺失」当成「没提交」而漏判清空语义。
func bindProductFormInto[T any](c *gin.Context, req *T) error {
	if c.Request != nil && c.Request.PostForm == nil {
		_ = c.Request.ParseMultipartForm(32 << 20)
	}
	if v, ok := any(req).(formPresenceValidator); ok {
		if err := v.ValidatePresence(c.Request.PostForm); err != nil {
			return err
		}
	}
	bound, err := binding.ParseForm[T](c)
	if err != nil {
		return err
	}
	*req = *bound
	return validateProductForm(req)
}

// runProductWriteResultJSON 是 runProductWriteResult 的显式 JSON 版本（表单是对象提供器）。
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

// runProductWriteJSON 用于**显式 JSON 输入**的写端点（复杂编辑器/对象提供器）。
//
// 前端 data-write-builder 交的是 JSON 对象（write-base 会设 Content-Type: application/json），
// 用 FormPost 绑定只会拿到空结构 —— 两者协议必须对齐。
// 用 binding.ParseJSON（显式 JSON，不按 Content-Type 自动选择协议）：端点声明的输入协议
// 必须与其实际接受的一致，不能让同一端点悄悄接受两种形状。
func runProductWriteJSON[T any](c *gin.Context,
	call func(context.Context, *T) (any, error),
	success func(*gin.Context)) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseJSON[T](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if err := validateProductForm(req); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return
	}
	if _, err := call(c.Request.Context(), req); err != nil {
		pageview.FailScene(c, productErrScene, err)
		return
	}
	success(c)
}

// validateProductForm 跑表单级校验（未实现 Validate 的 DTO 直接通过）。
func validateProductForm[T any](req *T) error {
	if v, ok := any(req).(formValidator); ok {
		return v.Validate()
	}
	return nil
}

// bindJSONFact 显式绑定 JSON 对象并跑表单级校验；错误事实交回调用方决定出口。
func bindJSONFact[T any](c *gin.Context) (*T, error) {
	req, err := binding.ParseJSON[T](c)
	if err != nil {
		return req, err
	}
	return req, validateProductForm(req)
}

// runProductWriteResultJSON 用于「JSON 绑定 → 注入可信字段 → 调用 → 渲染结果片段」。
func runProductWriteResultJSON[T any](c *gin.Context,
	prepare func(*gin.Context, *T),
	call func(context.Context, *T) (any, error),
	render func(*gin.Context, any)) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseJSON[T](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
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

// bindProductJSON 显式绑定 JSON 对象；失败已由这里写出 400。
func bindProductJSON[T any](c *gin.Context) *T {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseJSON[T](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, productErrScene, err)
		return nil
	}
	return req
}

// productWriteOK 是纯成功的公共出口。
func productWriteOK(c *gin.Context) { pageview.Success(c) }
