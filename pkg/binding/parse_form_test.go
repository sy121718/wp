package binding

import (
	"bytes"
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

	req, err := ParseForm[parseFormReq](c)
	if err != nil {
		t.Fatalf("绑定不应失败: %v", err)
	}
	if req.ID != "" {
		t.Fatalf("查询串不得进入写表单，实际 id=%q", req.ID)
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
