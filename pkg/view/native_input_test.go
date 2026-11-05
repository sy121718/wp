package view

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// 原生绑定契约不做字段整形；浏览器是否提交 disabled、具名按钮等由真实浏览器用例验证。
func TestNativeFormPostInputSemantics(t *testing.T) {
	type input struct {
		ID       string   `form:"id"`
		Title    string   `form:"title"`
		Enabled  bool     `form:"enabled"`
		Tags     []string `form:"tags"`
		Quantity *int     `form:"quantity"`
		Intent   string   `form:"intent"`
	}
	zero := 0
	for _, tc := range []struct {
		name string
		body url.Values
		want input
	}{
		{"值与具名提交按钮", url.Values{"id": {"9007199254740993"}, "title": {"body"}, "enabled": {"true"}, "tags": {"a", "b"}, "quantity": {"0"}, "intent": {"save"}}, input{ID: "9007199254740993", Title: "body", Enabled: true, Tags: []string{"a", "b"}, Quantity: &zero, Intent: "save"}},
		{"未选与集合全取消", url.Values{}, input{}},
		{"显式清空与空数字", url.Values{"title": {""}, "enabled": {"false"}, "tags": {""}, "quantity": {""}}, input{Tags: []string{""}, Quantity: &zero}},
		{"标量重复取首值", url.Values{"enabled": {"false", "true"}, "title": {"first", "second"}}, input{Title: "first"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			var got input
			router.POST("/write", func(c *gin.Context) {
				if err := c.ShouldBindWith(&got, binding.FormPost); err != nil {
					t.Errorf("原生绑定失败：%v", err)
					c.Status(http.StatusBadRequest)
					return
				}
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodPost, "/write?title=query&enabled=true&tags=query&quantity=42", strings.NewReader(tc.body.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("FormPost 不得合并 query 或整形空值：status=%d got=%+v want=%+v", rec.Code, got, tc.want)
			}
		})
	}
}

func TestNativeBindingDoesNotInventNestedRowsOrCallValidate(t *testing.T) {
	router := gin.New()
	var got nativeInputValidationProbe
	router.POST("/write", func(c *gin.Context) {
		if err := c.ShouldBindWith(&got, binding.FormPost); err != nil {
			t.Errorf("绑定失败：%v", err)
		}
	})
	req := httptest.NewRequest(http.MethodPost, "/write", strings.NewReader("rows%5B0%5D.quantity=3"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(httptest.NewRecorder(), req)
	if len(got.Rows) != 0 || got.Validated {
		t.Fatalf("不得假设原生 binder 理解明细下标或自动调用 Validate：%+v", got)
	}
}

type nativeInputValidationProbe struct {
	Rows []struct {
		Quantity int `form:"quantity"`
	} `form:"rows"`
	Validated bool `form:"-"`
}

func (p *nativeInputValidationProbe) Validate() error {
	p.Validated = true
	return nil
}

func TestNativeQueryBindingIgnoresWriteBody(t *testing.T) {
	router := gin.New()
	var got struct {
		Page int `form:"page"`
	}
	router.POST("/query", func(c *gin.Context) {
		if err := c.ShouldBindQuery(&got); err != nil {
			t.Errorf("查询绑定失败：%v", err)
		}
	})
	req := httptest.NewRequest(http.MethodPost, "/query?page=2", strings.NewReader("page=9"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(httptest.NewRecorder(), req)
	if got.Page != 2 {
		t.Fatalf("ShouldBindQuery 误读写 body：%d", got.Page)
	}
}

func TestNativeMultipartPreservesFileAndRepeatedFields(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, tag := range []string{"a", "b"} {
		if err := writer.WriteField("tags", tag); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("file", "sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, "upload-content"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Tags []string              `form:"tags"`
		File *multipart.FileHeader `form:"file"`
	}
	router := gin.New()
	router.POST("/upload", func(c *gin.Context) {
		if err := c.ShouldBindWith(&got, binding.FormMultipart); err != nil {
			t.Errorf("multipart 绑定失败：%v", err)
			return
		}
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		upload, err := got.File.Open()
		if err != nil {
			t.Error(err)
			return
		}
		defer upload.Close()
		content, err := io.ReadAll(upload)
		if err != nil || string(content) != "upload-content" {
			t.Errorf("上传内容丢失：%q，%v", content, err)
		}
	})
	req := httptest.NewRequest(http.MethodPost, "/upload?tags=query", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(httptest.NewRecorder(), req)
	if got.File == nil || got.File.Filename != "sample.txt" || !reflect.DeepEqual(got.Tags, []string{"a", "b"}) {
		t.Fatalf("multipart 字段丢失或合并 query：%+v", got)
	}
}
