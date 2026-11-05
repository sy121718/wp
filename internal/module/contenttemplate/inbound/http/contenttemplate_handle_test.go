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

	"go_wp/internal/adminui"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"

	"go_wp/internal/templates"
	"go_wp/internal/text"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
)

type ctDeleteStub struct {
	contenttemplatecontract.ContentTemplateService
	calls []string
}

func (s *ctDeleteStub) Delete(_ context.Context, req *contenttemplatedto.DeleteReq) error {
	s.calls = append(s.calls, req.ID)
	if req.ID == "blocked" {
		return errors.New("ErrTemplateInUse")
	}
	if req.ID == "broken" {
		return errors.New(`pq: relation "content_templates" does not exist (SQLSTATE 42P01)`)
	}
	return nil
}

func callContentTemplateDelete(t *testing.T, s *ctDeleteStub, form url.Values) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	r.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	r.POST("/api/contenttemplate/delete", pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), NewHandle(s).Delete)
	req := httptest.NewRequest(http.MethodPost, "/api/contenttemplate/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestContentTemplateDeleteForm(t *testing.T) {
	manyIDs := make(url.Values)
	for i := 0; i < 201; i++ {
		manyIDs.Add("ids", fmt.Sprintf("id-%d", i))
	}
	for _, tc := range []struct {
		name    string
		form    url.Values
		code    int
		calls   []string
		deleted int
		skipped int
	}{
		{"single", url.Values{"id": {"one"}}, 200, []string{"one"}, 1, 0},
		{"mixed and deduplicated", url.Values{"ids": {" one ", "blocked", "one", "broken", "two"}}, 200, []string{"one", "blocked", "broken", "two"}, 2, 2},
		{"empty", url.Values{"ids": {" ", ""}}, 200, nil, 0, 0},
		{"invalid", url.Values{"id": {" "}}, 400, nil, 0, 0},
		{"too many", manyIDs, 400, nil, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &ctDeleteStub{}
			code, body := callContentTemplateDelete(t, stub, tc.form)
			if code != tc.code {
				t.Fatalf("status=%d body=%s", code, body)
			}
			if fmt.Sprint(stub.calls) != fmt.Sprint(tc.calls) {
				t.Errorf("calls=%v want %v", stub.calls, tc.calls)
			}
			if strings.Contains(body, "SQLSTATE") || strings.Contains(body, "content_templates") {
				t.Fatalf("internal error leaked: %s", body)
			}
			if code != 200 {
				if !strings.Contains(body, "data-ui-error") {
					t.Fatalf("失败应渲染错误区：%s", body)
				}
				return
			}
			want := fmt.Sprintf("已删除 %d 项，跳过 %d 项", tc.deleted, tc.skipped)
			if !strings.Contains(body, want) {
				t.Fatalf("逐条结果缺失 %q：%s", want, body)
			}
		})
	}
}
