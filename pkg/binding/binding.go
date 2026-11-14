package binding

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/pkg/response"
)

type FormBinder interface {
	BindForm(url.Values) error
}

func JSONLimited[T any](c *gin.Context, maxBytes int64) (*T, bool) {
	if maxBytes <= 0 || c.Request == nil || c.Request.Body == nil {
		response.ParamError(c)
		return nil, false
	}

	reader := &io.LimitedReader{R: c.Request.Body, N: maxBytes}
	body, err := io.ReadAll(reader)
	var extra [1]byte
	n, tailErr := io.ReadFull(c.Request.Body, extra[:])
	if err != nil || n > 0 || (tailErr != nil && tailErr != io.EOF) {
		response.ParamError(c)
		return nil, false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return JSON[T](c)
}

// isMultipartBody 判断请求体声明的编码是否为 multipart/form-data。
// 只看 Content-Type，不解析表单内容 —— 它与 FormPost 一样只读 body。
func isMultipartBody(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data")
}

func isFormBody(c *gin.Context) bool {
	if c.Request == nil {
		return false
	}
	ct := strings.ToLower(c.GetHeader("Content-Type"))
	return strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data")
}

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

// JSON 是兼容入口：写方法按 Content-Type 在 JSON 与表单之间选择，读方法读查询串。
//
// 它服务的是一批**同时接受 JSON 与表单**的公开端点（访客下单、退货申请、站点账号），
// 这些端点的调用方既有 JSON 客户端也有 htmx 表单，不能收窄成单一协议。
// 正因为它不声明唯一协议，契约生成器把它标成 unknown —— 要声明协议请用
// ParseJSON（JSON body）或 ParseForm / BindForm（表单 body）。
func JSON[T any](c *gin.Context) (*T, bool) {
	var req T

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

	if isFormBody(c) {
		if b, ok := any(&req).(FormBinder); ok {
			if err := b.BindForm(bodyValues(c)); err != nil {
				response.ParamError(c)
				return nil, false
			}
		}
	}
	return &req, true
}

type QueryBinder interface {
	BindQuery(url.Values) error
}

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
	if c == nil || c.Request == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "application/x-www-form-urlencoded")
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

// Form 是兼容入口：按 Content-Type 选择 binder 并合并查询串。
//
// 与 JSON 同理，它不声明唯一协议，契约生成器标成 unknown。
// 要声明协议请用 ParseForm（只读 body 的表单）或 Query（只读查询串）。
func Form[T any](c *gin.Context) (*T, bool) {
	var req T
	if err := c.ShouldBind(&req); err != nil {
		response.ParamError(c)
		return nil, false
	}
	if b, ok := any(&req).(FormBinder); ok {
		if err := b.BindForm(bodyValues(c)); err != nil {
			response.ParamError(c)
			return nil, false
		}
	}
	return &req, true
}

type listQueryTagger interface {
	setListQuery(string)
}

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
