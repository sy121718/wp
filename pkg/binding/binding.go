// Package binding 是控制器读取请求参数的唯一入口。
//
// 控制器里只允许出现下面三种形态，其余一律视为手工解析：
//
//	req, ok := binding.JSON[productdto.CreateReq](c)   // 写动作：JSON 体
//	req, ok := binding.Query[productdto.ListReq](c)   // 页面 / 接口的查询串
//	req, ok := binding.Form[productdto.RowsReq](c)    // 页面片段的表单 POST
//
// 失败时本包已经写好 400 响应（pkg/response 的 {code,message,data}），调用方只写
// if !ok { return } —— 每个 handler 各写一遍「绑定失败怎么办」正是错误出口增生的起点。
//
// **DTO 必须带 form tag**：gin 的表单绑定按 tag 取键，字段没有 tag 时它去查键 ""，
// 于是整段绑定**静默返回零值而不报错**（Page 恒 0、Keyword 恒空）。这个坑看起来像
// 「前端没传」，排查成本极高，所以 tag 是 DTO 的契约，不是可选项。
//
// 反射绑定覆盖不到的形态（values[n].id 这种带下标的数组、attr:<id> 这种带前缀的多值）
// 由 DTO 自己实现 FormBinder，字段口径仍然留在 DTO 里，控制器不碰 url.Values。
package binding

import (
	"net/url"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/response"
)

// FormBinder 是反射绑定覆盖不到的表单形态的补充入口。
//
// 只在 Form 里被调用一次，收的是**已经解析好**的 url.Values —— 实现方不要自己
// ParseForm：gin 的 PostForm 是惰性填充的，绕过它再解析一遍会拿到空 map。
type FormBinder interface {
	BindForm(url.Values) error
}

// JSON 绑定 JSON 请求体。
func JSON[T any](c *gin.Context) (*T, bool) {
	var req T
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c)
		return nil, false
	}
	return &req, true
}

// Query 绑定查询串（GET 的参数）。
func Query[T any](c *gin.Context) (*T, bool) {
	var req T
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c)
		return nil, false
	}
	return &req, true
}

// Form 绑定表单体（urlencoded 或 multipart），并在 DTO 实现 FormBinder 时补一遍。
func Form[T any](c *gin.Context) (*T, bool) {
	var req T
	if err := c.ShouldBind(&req); err != nil {
		response.ParamError(c)
		return nil, false
	}
	if b, ok := any(&req).(FormBinder); ok {
		if err := b.BindForm(c.Request.PostForm); err != nil {
			response.ParamError(c)
			return nil, false
		}
	}
	return &req, true
}
