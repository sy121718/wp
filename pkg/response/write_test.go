package response

// write_test.go — 响应头里的 message 副本。
//
// 判据三条：
//  1. body **完全不变**（{code,message,data} 是 AGENTS 写死的契约，MCP/AI/fetch 都在读它）；
//  2. 头里的值能还原成 body 里那一句（中文要能过 HTTP 头，所以走 QueryEscape）；
//  3. 头必须在 WriteHeader 之前写 —— 顺序错了会被 net/http 静默丢掉。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestWriteResponseSetsMessageHeader body 与头同时有，头可还原成 body 那句。
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
	// body 仍是 JSON 契约
	if ct := w.Header().Get("Content-Type"); ct == "" {
		t.Fatalf("body 应仍是 JSON（Content-Type 为空）")
	}
}

// TestWriteResponseEscapesChinese 中文头值必须是纯 ASCII（HTTP 头不允许裸中文）。
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

// TestWriteResponseEmptyMessageNoHeader 空文案不写头（避免 `X-Message: ` 这种空值）。
func TestWriteResponseEmptyMessageNoHeader(t *testing.T) {
	c, w := newCtx()
	writeResponse(c, http.StatusOK, Response{Code: http.StatusOK})
	if raw := w.Header().Get(HeaderMessage); raw != "" {
		t.Fatalf("空文案不该写头，实际 %q", raw)
	}
}

// TestExitsAllGoThroughWriteResponse 四个出口都必须走统一落点。
//
// 这条挡的是「以后有人在新出口里直接 c.JSON」——那个响应就没有 message 头，
// 而前端只会看到「点了没提示」，两处都不报错。
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
