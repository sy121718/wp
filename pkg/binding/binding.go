package binding

import (
	"errors"
	"mime"
	"net/url"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"
)

type FormBinder interface {
	BindForm(url.Values) error
}

// formMediaType 解析请求声明的媒体类型，并回答它是否就是两种表单编码之一。
//
// 必须解析后精确比较，不能用前缀匹配：`application/x-www-form-urlencoded+json` 与
// `application/x-www-form-urlencodedgarbage` 都以表单前缀开头，前缀匹配会把它们
// 当成表单，于是非表单请求体退回零值表单并静默成功 —— 带默认值的 DTO（偏好页大小、
// 通知开关）会把「用户没提交」写成「用户要求默认值」。
func formMediaType(c *gin.Context) (string, bool) {
	if c == nil || c.Request == nil {
		return "", false
	}
	raw := strings.TrimSpace(c.GetHeader("Content-Type"))
	if raw == "" {
		return "", false
	}
	mt, _, err := mime.ParseMediaType(raw)
	if err != nil {
		// 媒体类型本身不合法（含 `;garbage` 之外的畸形串）：无法证明它是表单。
		return "", false
	}
	switch mt {
	case "multipart/form-data", "application/x-www-form-urlencoded":
		return mt, true
	default:
		return mt, false
	}
}

// isMultipartBody 判断请求体声明的编码是否为 multipart/form-data。
// 只看 Content-Type，不解析表单内容 —— 它与 FormPost 一样只读 body。
func isMultipartBody(c *gin.Context) bool {
	mt, ok := formMediaType(c)
	return ok && mt == "multipart/form-data"
}

type QueryBinder interface {
	BindQuery(url.Values) error
}

func Field(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

func ParseJSON[T any](c *gin.Context) (*T, error) {
	var req T
	return &req, c.ShouldBindJSON(&req)
}

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
	applyListQuery(c, &req)
	return &req, nil
}

// ErrFormContentType 表示请求声明的编码不是表单。
//
// 单独给一个可判定的错误：调用方统一回 400 即可，不需要解析错误文案。
var ErrFormContentType = errors.New("binding: 表单端点只接受 application/x-www-form-urlencoded 或 multipart/form-data")

// ParseForm 是显式表单绑定：只读请求体，不合并查询串。
//
// ShouldBind 会按 Content-Type 自动选择 binder 并把 query 并进表单，
// 于是 `POST /save?id=x` 在 body 为空时也能绑出 id —— 写端点与查询串的边界就没了。
// 这里按声明的编码选 FormPost 或 FormMultipart，两者都只读 body：
// 字段只可能来自请求体，取不到就是零值，交给调用方显式校验。
//
// 非表单编码**直接报错**，不能退回零值表单：Gin 的表单 binder 对 JSON 体
// 只会得到空值，若当成绑定成功，带默认值的 DTO（偏好页大小、通知开关）
// 会把「用户没提交」写成「用户要求默认值」，等于用错误编码覆盖真实设置。
// 没有 Content-Type 的请求同样拒绝 —— 无法证明它是表单，就不按表单处理。
func ParseForm[T any](c *gin.Context) (*T, error) {
	var req T
	var binder ginbinding.Binding
	switch {
	case isMultipartBody(c):
		binder = ginbinding.FormMultipart
	case isURLEncodedBody(c):
		binder = ginbinding.FormPost
	default:
		return &req, ErrFormContentType
	}
	if err := c.ShouldBindWith(&req, binder); err != nil {
		return &req, err
	}
	if b, ok := any(&req).(FormBinder); ok {
		if err := b.BindForm(bodyValues(c)); err != nil {
			return &req, err
		}
	}
	return &req, nil
}

// isURLEncodedBody 判断请求体是否为 urlencoded 表单。
func isURLEncodedBody(c *gin.Context) bool {
	mt, ok := formMediaType(c)
	return ok && mt == "application/x-www-form-urlencoded"
}

// bodyValues 返回请求体里的表单值：urlencoded 用 PostForm，
// multipart 解析后值在 MultipartForm.Value，PostForm 不会自动合并。
// 两者都只来自 body，不混入查询串。
func bodyValues(c *gin.Context) url.Values {
	if c == nil || c.Request == nil {
		return url.Values{}
	}
	if c.Request.MultipartForm != nil {
		return url.Values(c.Request.MultipartForm.Value)
	}
	return c.Request.PostForm
}

type listQueryTagger interface {
	setListQuery(string)
}

func listQueryTag(req any) []string {
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

func applyListQuery(c *gin.Context, req any) {
	keys := listQueryTag(req)
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
