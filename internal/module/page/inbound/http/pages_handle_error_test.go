package pagehttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/response"
)

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
	router.GET("/api/page/redirect", pageview.Slot(RedirectPageTemplate, pageview.RenderPage), NewHandle(&redirectListFailStub{err: rawErr}).RedirectPage)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/page/redirect?project=p1&lang=zh-CN", nil))
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("取数失败的状态码应为 500，得到 %d：%s", rec.Code, body)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("失败出口应为 JSON，得到 %q", ct)
	}
	for _, banned := range []string{"<!DOCTYPE", "</body>", "</html>"} {
		if strings.Contains(body, banned) {
			t.Errorf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
		}
	}
	if !strings.Contains(body, response.InternalFallback) {
		t.Errorf("失败响应应带归口文案：%s", body)
	}
	if strings.Contains(body, rawErr.Error()) {
		t.Errorf("内部错误原文漏进了响应：%s", body)
	}
}
