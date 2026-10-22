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
	"net/http"
	"net/url"
	"reflect"
	"strings"

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

// JSON 绑定请求体，**同时接受 JSON 与表单体**（按 Content-Type 自适应）。
//
// 名字里的 JSON 是历史叫法（写 API 的主契约仍是 JSON）；实际上用的是 gin 的
// `ShouldBind` —— 它按 Content-Type 分派：application/json 走 JSON，
// x-www-form-urlencoded / multipart 走表单。
//
// 为什么需要这条：写动作现在有**两个**客户端 —— MCP / AI / fetch 发 JSON，
// htmx 的写表单发 formdata。同一端点同时服务两者时，若用 `ShouldBindJSON`，
// htmx 发来的表单会被当成"JSON 解析失败"整条 400（实测：字段全为零值）。
//
// 表单体能否绑上取决于 DTO 的 `form` tag：
//   - 有 `form:"tagIds"` → 能收到同名的多值（tagIds=a&tagIds=b → [a b]）、
//     指针、内嵌 struct；
//   - 没 `form` tag → gin 按**字段名**匹配（大小写不敏感），`ProjectID` 能收到
//     `ProjectID`/`projectID`，但**收不到 `projectId`** —— 而 JSON tag 通常就是
//     小驼峰。所以新写的写 DTO 请把 `form` tag 一并写上（与 json tag 同名即可）。
//
// —— 一个必须显式处理的边界 ——
//
// `ShouldBind` 在**没有 Content-Type** 时一律当表单处理（gin 的 formBinding 不看 CT）。
// 于是一段畸形 JSON（`{not json`）会被当成表单体而"绑定成功"（键名乱但不是错），
// 这正是本函数改成自适应后唯一放宽的地方。真实浏览器发 JSON 一定会带
// `Content-Type: application/json`，所以这个口子只在"客户端不守规矩"时出现；
// 但写接口不能靠客户端自觉 —— 下面显式拦回去：带了 body 却没有 JSON 声明的，
// 按 JSON 解析，解析不了就 400（与旧行为一致）。
//
// 其他边界照旧：`binding:"required"` 这类校验 tag 照样生效（ShouldBind 绑完仍跑
// 校验），空 body、非法 JSON 也照样报错。
func JSON[T any](c *gin.Context) (*T, bool) {
	var req T
	// 没声明 Content-Type 但有 body：当 JSON 处理，保住旧行为。
	if hasBodyWithoutContentType(c) {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.ParamError(c)
			return nil, false
		}
		return &req, true
	}
	if err := c.ShouldBind(&req); err != nil {
		response.ParamError(c)
		return nil, false
	}
	return &req, true
}

// hasBodyWithoutContentType 请求带了 body 但没声明 Content-Type。
//
// 只看 POST/PUT/PATCH：GET/DELETE 没有 body 是正常的，不该因此报错。
func hasBodyWithoutContentType(c *gin.Context) bool {
	if c.Request == nil || c.Request.Body == nil {
		return false
	}
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return false
	}
	return strings.TrimSpace(c.GetHeader("Content-Type")) == ""
}

// QueryBinder 是查询串里反射绑定覆盖不到的形态的补充入口。
//
// 典型场景只有一类：**同一个参数有两套键名**（页面入口同时接受 project 与 projectId），
// 而结构体字段只能挂一个 tag。键名兼容属于「请求形状」，留在 DTO 里；
// 放控制器就等于每加一个入口再抄一遍。
type QueryBinder interface {
	BindQuery(url.Values) error
}

// Query 绑定查询串（GET 的参数），并在 DTO 实现 QueryBinder 时补一遍。
func Query[T any](c *gin.Context) (*T, bool) {
	var req T
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c)
		return nil, false
	}
	if b, ok := any(&req).(QueryBinder); ok {
		if err := b.BindQuery(c.Request.URL.Query()); err != nil {
			response.ParamError(c)
			return nil, false
		}
	}
	ApplyListQuery(c, &req)
	return &req, true
}

// Field 读取**跨表单共享的隐藏域**（next / returnUrl / requestId 这类）。
//
// 它不是「手工读取业务字段」的后门：业务字段一律进 DTO 的 form tag。
// 存在的理由是这些键**不属于任何一张表单** —— 同一个 next 会跟着登录、注册、
// 重置密码三张表单走，给每个 DTO 都抄一遍只会让三处各自漂移。
func Field(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

// ParseJSON 绑定 JSON 请求体，**不写响应**，把失败原样交回调用方。
//
// 失败时返回值**不是 nil**：要求是「就地回显表单」的页面出口要拿已经绑上的字段
// （漏填目标路径时仍要显示已填的源路径），把 nil 传进去会清空整张表单。
// 调用方判失败只看 error，不要把返回值当 nil 判据。
//
// 只给出口不是「400 + pkg/response 报文」的形态用，三种真实场景：
//   - JSON-RPC（失败的响应体是 rpc error 对象，不是 {code,message,data}）
//   - 页面片段（失败要渲染进整页 / 片段提示，不是甩一个 JSON）
//   - 绑定成功后还要按形状再分流（顶层键白名单、去 null 后判空）
//
// 其余一律用 JSON / Query / Form —— 那些形态的 400 文案与状态码只有一份。
func ParseJSON[T any](c *gin.Context) (*T, error) {
	var req T
	return &req, c.ShouldBindJSON(&req)
}

// ParseQuery 绑定查询串，不写响应。语义见 ParseJSON。
func ParseQuery[T any](c *gin.Context) (*T, error) {
	var req T
	if err := c.ShouldBindQuery(&req); err != nil {
		return &req, err
	}
	if b, ok := any(&req).(QueryBinder); ok {
		if err := b.BindQuery(c.Request.URL.Query()); err != nil {
			return &req, err
		}
	}
	return &req, nil
}

// ParseForm 绑定表单体（urlencoded 或 multipart），不写响应；DTO 实现 FormBinder 时同样补一遍。
func ParseForm[T any](c *gin.Context) (*T, error) {
	var req T
	if err := c.ShouldBind(&req); err != nil {
		return &req, err
	}
	if b, ok := any(&req).(FormBinder); ok {
		if err := b.BindForm(c.Request.PostForm); err != nil {
			return &req, err
		}
	}
	return &req, nil
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

// listQueryTagger 由带 ListQuery 字段的查询 DTO 实现（tag 形式给出「哪些键属于列表上下文」）。
//
// 为什么不在控制器里写 `req.ListQuery = binding.ListQuery(c, keys...)`：那是每个列表入口
// 一行同样的装配，键表还得在控制器里再声明一次。键表属于「这一屏的请求形状」，落在 DTO 的
// struct tag 上只有一份：
//
// \tListQuery string `form:"-" listquery:"project,keyword,page,limit"`
//
// binding.Query 绑定完查询串后按 tag 填好它，入口就不必再碰请求串。
type listQueryTagger interface {
	setListQuery(string)
}

// ListQueryTag 读 DTO 上的 listquery tag（键以逗号分隔，顺序即回填链接的顺序）。
func ListQueryTag(req any) []string {
	t := reflect.TypeOf(req)
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	f, ok := t.FieldByName("ListQuery")
	if !ok {
		return nil
	}
	return splitKeys(f.Tag.Get("listquery"))
}

// ApplyListQuery 按 DTO 的 listquery tag 填 ListQuery（tag 为空时什么都不做）。
func ApplyListQuery(c *gin.Context, req any) {
	keys := ListQueryTag(req)
	if len(keys) == 0 {
		return
	}
	rv := reflect.ValueOf(req)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return
	}
	f := rv.FieldByName("ListQuery")
	if !f.IsValid() || f.Kind() != reflect.String || !f.CanSet() {
		return
	}
	f.SetString(ListQuery(c, keys...))
}

func splitKeys(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if k := strings.TrimSpace(p); k != "" {
			out = append(out, k)
		}
	}
	return out
}
