package pubhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	pubenums "go_wp/internal/module/publication/enums"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/templates"
	"go_wp/pkg/facing"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"

	"github.com/gin-gonic/gin"
)

const rawLeak = `<img src=x onerror=alert(1)> & "quote" 'tick' ` + "`backtick`" + ` private-path:/srv/artifacts/secret`

func seoAuditEngine(run func(context.Context, string) (pubservice.AuditResult, error)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	r.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	r.POST("/api/publication/seo-audit", pageview.HTMLFragment, NewSeoAuditHandle(run).SeoAudit)
	return r
}

func assertNoRedirect(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code >= 300 && w.Code < 400 {
		t.Fatalf("写动作不得重定向，实际 %d Location=%q", w.Code, w.Header().Get("Location"))
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("写动作不得给 Location，实际 %q", loc)
	}
	if hx := w.Header().Get("HX-Redirect"); hx != "" {
		t.Fatalf("写动作不得给 HX-Redirect，实际 %q", hx)
	}
}

func TestSEOAuditHTMLContract(t *testing.T) {
	issue := pubservice.AuditIssue{Path: "/a", Item: "title_missing", Level: "error"}
	ok := pubservice.AuditResult{Scanned: 2, Count: 1, Issues: []pubservice.AuditIssue{issue}}
	clean := pubservice.AuditResult{Scanned: 2, Count: 0, Issues: []pubservice.AuditIssue{}}

	cases := []struct {
		name        string
		form        url.Values
		runErr      error
		result      pubservice.AuditResult
		wantStatus  int
		wantIssues  bool
		wantClean   bool
		wantNoCall  bool
		wantProject string
	}{
		{
			name: "合法表单按工程运行", form: url.Values{"project": {"p1"}},
			result: ok, wantStatus: http.StatusOK, wantIssues: true, wantProject: "p1",
		},
		{
			name: "没有问题走 clean 档", form: url.Values{"project": {"p1"}},
			result: clean, wantStatus: http.StatusOK, wantClean: true, wantProject: "p1",
		},
		{
			name: "缺工程由 service 拒绝", form: url.Values{},
			runErr: pubenums.ErrProjectMissing(), wantStatus: http.StatusBadRequest, wantProject: "",
		},
		{
			name: "业务错误按声明的状态码", form: url.Values{"project": {"p1"}},
			runErr: pubenums.ErrProjectMissing(), wantStatus: http.StatusBadRequest, wantProject: "p1",
		},
		{
			name: "基础设施错误 500 且不泄漏原文", form: url.Values{"project": {"p1"}},
			runErr: errors.New("open /srv/artifacts: permission denied"), wantStatus: http.StatusInternalServerError, wantProject: "p1",
		},
		{
			name: "声明了 409 的错误按 409", form: url.Values{"project": {"p1"}},
			runErr:     facing.WithStatus(pubenums.Err(pubenums.ErrReceiptPending), http.StatusConflict),
			wantStatus: http.StatusConflict, wantProject: "p1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotProject string
			callCount := 0
			run := func(_ context.Context, id string) (pubservice.AuditResult, error) {
				callCount++
				gotProject = id
				if tc.runErr != nil {
					return pubservice.AuditResult{}, tc.runErr
				}
				return tc.result, nil
			}
			r := seoAuditEngine(run)
			req := httptest.NewRequest(http.MethodPost, "/api/publication/seo-audit", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("状态码 = %d，want %d，body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			assertNoRedirect(t, w)
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("界面响应必须是 HTML，实际 %q", w.Header().Get("Content-Type"))
			}
			if callCount != 1 || gotProject != tc.wantProject {
				t.Fatalf("service 调用次数 = %d，工程 = %q，want %q", callCount, gotProject, tc.wantProject)
			}
			body := w.Body.String()
			if tc.wantStatus != http.StatusOK {
				if !strings.Contains(body, "data-ui-error") {
					t.Fatalf("失败应渲染错误区：%s", body)
				}
				for _, leak := range []string{rawLeak, "private-path", "/srv/artifacts", "permission denied"} {
					if strings.Contains(body, leak) {
						t.Fatalf("响应泄漏内部原文 %q：%s", leak, body)
					}
				}
				return
			}
			if tc.wantIssues {
				if !strings.Contains(body, `data-seo-audit-state="issues"`) || !strings.Contains(body, "/a") {
					t.Fatalf("问题档应渲染路径：%s", body)
				}
				if strings.Contains(body, "{scanned}") || strings.Contains(body, "{count}") {
					t.Fatalf("参数句未替换：%s", body)
				}
				return
			}
			if tc.wantClean && !strings.Contains(body, `data-seo-audit-state="clean"`) {
				t.Fatalf("无问题应渲染 clean 档：%s", body)
			}
		})
	}
}

func TestSEOAuditEmptyArtifactsRendersEmptyState(t *testing.T) {
	run := func(context.Context, string) (pubservice.AuditResult, error) {
		return pubservice.AuditResult{Scanned: 0, Count: 0, Issues: []pubservice.AuditIssue{}}, nil
	}
	r := seoAuditEngine(run)
	req := httptest.NewRequest(http.MethodPost, "/api/publication/seo-audit", strings.NewReader("project=p1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `data-seo-audit-state="empty"`) {
		t.Fatalf("没有产物应渲染 empty 档：%d %s", w.Code, w.Body.String())
	}
}
