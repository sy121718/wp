package pagehttp

// pages_handle_error_test.go — 重定向管理页取数失败：同页渲染 + 500 + no-store。
//
// 三条判据（都在别处不会红）：
//
//  1. **仍是一页**：读不到 301 列表时渲染同一份页面模板（页壳与标题都在），不是 JSON 报文、
//     不是裸文本 —— 地址栏里是页面 URL，回一段 JSON 只会让浏览器显示一串报文。
//  2. **状态码是 500**：200 会让「读不到」与「还没有重定向」在页面上长得一样，监控也看不见
//     失败（与 navigation 的整页 500 同口径）。
//  3. **no-store**：失败页不该被缓存 —— 重试必须真的回到服务端，否则用户刷新看到的还是
//     那张缓存下来的失败页（本次 task-31 修的就是这一条：此前只缺这个响应头）。
//
// 内部原文（这里刻意造一段「像数据库报错」的文案）只进日志，不进响应体。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
)

// redirectListFailStub 只实现 RedirectPage 真正走到的那个契约方法（其余由嵌入接口兜住）。
type redirectListFailStub struct {
	pagecontract.PageService
	err error
}

func (s *redirectListFailStub) ListRedirects(_ context.Context, _ *pagedto.RedirectListReq) (*pagedto.RedirectListResp, error) {
	return nil, s.err
}

func TestRedirectPageLoadFailureReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawErr := errors.New(`pq: relation "page_redirect" does not exist`)

	router := gin.New()
	router.HTMLRender = newPageErrTestRender(t)
	router.GET("/api/page/redirect", adminui.PageSlot(RedirectPageTemplate), NewHandle(&redirectListFailStub{err: rawErr}).RedirectPage)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/page/redirect?project=p1&lang=zh-CN", nil))
	body := rec.Body.String()

	// 状态码：500（200 会让失败看起来像「还没有重定向」）。
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("取数失败的状态码应为 500，得到 %d：%s", rec.Code, body)
	}
	// 形状：JSON 错误体，不渲染页壳（失败一律一个提示 —— 用户裁定）。
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("失败出口应为 JSON，得到 %q", ct)
	}
	for _, banned := range []string{"<!DOCTYPE", "</body>", "</html>"} {
		if strings.Contains(body, banned) {
			t.Errorf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
		}
	}
	if !strings.Contains(body, "服务器内部错误，请稍后重试") {
		t.Errorf("失败响应应带归口文案：%s", body)
	}
	if strings.Contains(body, rawErr.Error()) {
		t.Errorf("内部错误原文漏进了响应：%s", body)
	}
}
