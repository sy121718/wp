package binding

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

type parseFormReq struct {
	ID string `form:"id"`
}

// 写端点只读 body：查询串里的 id 不能被当成表单字段。
func TestParseFormDoesNotMergeQueryString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/save?id=from-query", bytes.NewBufferString(""))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := ParseForm[parseFormReq](c)
	if err != nil {
		t.Fatalf("绑定不应失败: %v", err)
	}
	if req.ID != "" {
		t.Fatalf("查询串不得进入写表单，实际 id=%q", req.ID)
	}
}

// 非表单编码必须直接报错，不能退化成「绑定成功 + 全零值」。
//
// 这条判据来自一个真实的写入风险：偏好保存的表单钩子会把缺失的 pageSize
// 补成 20、通知开关补成 false。若 JSON 请求被当成空表单通过，用户提交的
// application/json 不会改任何字段，却把默认值完整写进偏好表 —— 用户看到的
// 是 200，数据库里是「被重置」。
func TestParseFormRejectsNonFormEncodings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, contentType, body string }{
		{"JSON", "application/json", `{"id":"from-json"}`},
		{"JSON 带 charset", "application/json; charset=utf-8", `{"id":"from-json"}`},
		{"纯文本", "text/plain", "id=from-text"},
		{"无 Content-Type", "", `{"id":"from-json"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/save", bytes.NewBufferString(tc.body))
			if tc.contentType != "" {
				c.Request.Header.Set("Content-Type", tc.contentType)
			}
			req, err := ParseForm[parseFormReq](c)
			if !errors.Is(err, ErrFormContentType) {
				t.Fatalf("非表单编码应返回 ErrFormContentType，实际 err=%v req=%+v", err, req)
			}
		})
	}
}

// multipart 与 urlencoded 都仍是合法表单：拒绝面只针对「声明的不是表单」。
func TestParseFormAcceptsBothFormEncodings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, contentType := range []string{
		"application/x-www-form-urlencoded",
		"application/x-www-form-urlencoded; charset=utf-8",
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/save", bytes.NewBufferString("id=from-body"))
		c.Request.Header.Set("Content-Type", contentType)
		req, err := ParseForm[parseFormReq](c)
		if err != nil || req.ID != "from-body" {
			t.Fatalf("%s 应正常绑定：%+v err=%v", contentType, req, err)
		}
	}
}

func TestParseFormReadsURLEncodedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := url.Values{"id": {"from-body"}}.Encode()
	req0 := httptest.NewRequest(http.MethodPost, "/save", bytes.NewBufferString(body))
	req0.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.Request = req0

	req, err := ParseForm[parseFormReq](c)
	if err != nil || req.ID != "from-body" {
		t.Fatalf("应读到 body 字段：%+v err=%v", req, err)
	}
}

func TestParseFormReadsMultipartBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("id", "from-multipart"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req0 := httptest.NewRequest(http.MethodPost, "/save", &buf)
	req0.Header.Set("Content-Type", writer.FormDataContentType())
	c.Request = req0

	req, err := ParseForm[parseFormReq](c)
	if err != nil || req.ID != "from-multipart" {
		t.Fatalf("应读到 multipart body 字段：%+v err=%v", req, err)
	}
}

type formHookReq struct {
	Note string `form:"note"`
}

func (r *formHookReq) BindForm(v url.Values) error {
	r.Note = v.Get("note")
	return nil
}

var _ FormBinder = (*formHookReq)(nil)

// BindForm 钩子在 multipart 请求里也要拿到 body 值（值在 MultipartForm.Value）。
func TestParseFormHookSeesMultipartValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("note", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req0 := httptest.NewRequest(http.MethodPost, "/save", &buf)
	req0.Header.Set("Content-Type", writer.FormDataContentType())
	c.Request = req0

	req, err := ParseForm[formHookReq](c)
	if err != nil || req.Note != "hello" {
		t.Fatalf("BindForm 钩子应收到 multipart body 值：%+v err=%v", req, err)
	}
}
