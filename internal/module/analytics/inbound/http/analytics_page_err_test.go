package analyticshttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

const analyticsProjectListErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

type analyticsFailingProjects struct {
	projectcontract.ProjectService
}

func (analyticsFailingProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, errors.New(analyticsProjectListErrText)
}

func TestAnalyticsPageDegradesWhenProjectListFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handle := NewAnalyticsPageHandle(nil, analyticsFailingProjects{})
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/analytics", adminui.PageSlot(AnalyticsPageTemplate), handle.AnalyticsPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/analytics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染页面（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, "</html>") {
		t.Fatalf("装载失败后页面必须渲染完整（缺 </html>）：%s", body)
	}

	if !strings.Contains(body, `role="alert"`) {
		t.Fatal("装载失败后提示条必须还在（role=alert）")
	}

	for _, want := range []string{"统计服务内部错误，请稍后重试", "工程列表没读出来"} {
		if !strings.Contains(body, want) {
			t.Errorf("装载失败页面缺少受控提示 %q", want)
		}
	}
	for _, forbidden := range []string{
		"MsgInternalError",
		"ErrAnalyticsInternal",
		"SQLSTATE", "projects\"", "42P01",
		"还没有站点工程",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("装载失败页面不应出现 %q", forbidden)
		}
	}
}
