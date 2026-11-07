package contenttemplatehttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"

	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"

	"github.com/gin-gonic/gin"
)

type ctDeleteStub struct {
	contenttemplatecontract.ContentTemplateService
	calls [][]string
	err   error
}

func (s *ctDeleteStub) BulkDelete(_ context.Context, req *contenttemplatedto.BulkDeleteReq) error {
	s.calls = append(s.calls, append([]string(nil), req.IDs...))
	return s.err
}

func callContentTemplateDelete(t *testing.T, s *ctDeleteStub, form url.Values) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	r.POST("/api/contenttemplate/delete", pageview.HTMLFragment, pageview.UseRenderer(pageview.RenderPage), NewHandle(s).Delete)
	req := httptest.NewRequest(http.MethodPost, "/api/contenttemplate/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestContentTemplateDeleteForm 锁住控制器只做形状与互斥判断：
// 原始 ids 原样交给一次 BulkDelete（去重、上限、整批拒绝都归 service），
// 成功只走共享成功出口，失败渲染错误区且不漏内部原文。
func TestContentTemplateDeleteForm(t *testing.T) {
	for _, tc := range []struct {
		name  string
		form  url.Values
		calls [][]string
	}{
		{"single", url.Values{"id": {"one"}}, [][]string{{"one"}}},
		// 控制器不预过滤：空项、重复项与非法形状都由 service 统一判定。
		{"bulk passed verbatim", url.Values{"ids": {" one ", "one", "", "two"}}, [][]string{{" one ", "one", "", "two"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &ctDeleteStub{}
			code, body := callContentTemplateDelete(t, stub, tc.form)
			if code != http.StatusOK {
				t.Fatalf("status=%d body=%s", code, body)
			}
			if fmt.Sprint(stub.calls) != fmt.Sprint(tc.calls) {
				t.Errorf("calls=%v want %v", stub.calls, tc.calls)
			}
			if !strings.Contains(body, "操作成功") {
				t.Fatalf("成功应走公共成功出口：%s", body)
			}
		})
	}
}

// TestContentTemplateDeleteRejectsUnshapedRequests 形状错在进 service 前拒绝。
func TestContentTemplateDeleteRejectsUnshapedRequests(t *testing.T) {
	for _, form := range []url.Values{
		{},
		{"id": {" "}},
		{"id": {"one"}, "ids": {"two"}},
	} {
		stub := &ctDeleteStub{}
		code, body := callContentTemplateDelete(t, stub, form)
		if code != http.StatusBadRequest {
			t.Fatalf("form=%v status=%d body=%s", form, code, body)
		}
		if len(stub.calls) != 0 {
			t.Fatalf("形状错的请求不该进 service：%v", stub.calls)
		}
		if !strings.Contains(body, "data-ui-error") {
			t.Fatalf("失败应渲染错误区：%s", body)
		}
	}
}

// TestContentTemplateDeleteServiceFailureDoesNotLeak 业务整批拒绝与非业务故障
// 都只给归口文案，原始驱动错误只进日志。
func TestContentTemplateDeleteServiceFailureDoesNotLeak(t *testing.T) {
	stub := &ctDeleteStub{err: errors.New(`pq: relation "content_templates" does not exist (SQLSTATE 42P01)`)}
	code, body := callContentTemplateDelete(t, stub, url.Values{"ids": {"a", "b"}})
	if code < http.StatusInternalServerError {
		t.Fatalf("非业务故障应 5xx，实际 %d：%s", code, body)
	}
	if strings.Contains(body, "SQLSTATE") || strings.Contains(body, "content_templates") {
		t.Fatalf("内部错误原文泄露：%s", body)
	}
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("失败应渲染错误区：%s", body)
	}
}
