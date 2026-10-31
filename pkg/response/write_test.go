package response

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/x", nil)
	return c, w
}

func TestWriteResponseSetsMessageHeader(t *testing.T) {
	c, w := newCtx()
	writeResponse(c, http.StatusOK, Response{Code: http.StatusOK, Message: "保存成功", Data: 1})

	raw := w.Header().Get(HeaderMessage)
	if raw == "" {
		t.Fatalf("缺少 %s 头", HeaderMessage)
	}
	got, err := url.QueryUnescape(raw)
	if err != nil {
		t.Fatalf("头值无法还原（说明没按 QueryEscape 编码）: %v", err)
	}
	if got != "保存成功" {
		t.Fatalf("头值还原成 %q，期望「保存成功」", got)
	}

	if ct := w.Header().Get("Content-Type"); ct == "" {
		t.Fatalf("body 应仍是 JSON（Content-Type 为空）")
	}
}

func TestWriteResponseEscapesChinese(t *testing.T) {
	c, w := newCtx()
	writeResponse(c, http.StatusBadRequest, Response{Code: 400, Message: "名称重复，请换一个"})
	raw := w.Header().Get(HeaderMessage)
	for i := 0; i < len(raw); i++ {
		if raw[i] > 127 {
			t.Fatalf("头值含非 ASCII 字节 %q（中文未编码，代理会丢或被拒）", raw)
		}
	}
}

func TestWriteResponseEmptyMessageNoHeader(t *testing.T) {
	c, w := newCtx()
	writeResponse(c, http.StatusOK, Response{Code: http.StatusOK})
	if raw := w.Header().Get(HeaderMessage); raw != "" {
		t.Fatalf("空文案不该写头，实际 %q", raw)
	}
}

func TestExitsAllGoThroughWriteResponse(t *testing.T) {
	cases := []struct {
		name string
		call func(c *gin.Context)
	}{
		{"Success", func(c *gin.Context) { Success(c) }},
		{"SuccessWithMessage", func(c *gin.Context) { SuccessWithMessage(c, "ok") }},
		{"ErrorWithMessage", func(c *gin.Context) { ErrorWithMessage(c, http.StatusBadRequest, "bad") }},
		{"ErrorWithData", func(c *gin.Context) { ErrorWithData(c, http.StatusBadRequest, "bad", nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := newCtx()
			tc.call(c)
			if w.Header().Get(HeaderMessage) == "" {
				t.Fatalf("%s 没有写 %s 头 —— 它没走 writeResponse", tc.name, HeaderMessage)
			}
		})
	}
}

func TestHeaderValuePairsWithDecodeURIComponent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"英文空格", "Product created successfully", "Product%20created%20successfully"},
		{"中文", "保存成功", "%E4%BF%9D%E5%AD%98%E6%88%90%E5%8A%9F"},
		{"字面量加号", "a+b", "a%2Bb"},
		{"百分号", "100%", "100%25"},
		{"混合", "a b+c", "a%20b%2Bc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeHeaderValue(tc.in)
			if got != tc.want {
				t.Fatalf("encodeHeaderValue(%q) = %q，期望 %q", tc.in, got, tc.want)
			}

			if strings.Contains(got, "+") {
				t.Fatalf("头值里出现裸 +：%q（空格应用 %%20，加号应用 %%2B）", got)
			}

			for i := 0; i < len(got); i++ {
				if got[i] > 127 {
					t.Fatalf("头值含非 ASCII 字节：%q", got)
				}
			}
		})
	}
}

func TestHeaderValueEmptyNoHeader(t *testing.T) {
	c, w := newCtx()
	writeResponse(c, http.StatusOK, Response{Code: http.StatusOK})
	if raw := w.Header().Get(HeaderMessage); raw != "" {
		t.Fatalf("空文案不该写头，实际 %q", raw)
	}
}
