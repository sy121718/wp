package binding

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/response"
)

type FormBinder interface {
	BindForm(url.Values) error
}

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
			if err := b.BindForm(c.Request.PostForm); err != nil {
				response.ParamError(c)
				return nil, false
			}
		}
	}
	return &req, true
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
