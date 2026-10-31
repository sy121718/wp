package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

func TestPagesListTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now()
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/pages", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin/page/pages", map[string]any{

			"title": "页面管理", "menu": "pages", "t": templates.TranslateFunc("en-US"),

			"Buttons":  map[string]any{"project.create": true, "page.create": true},
			"Projects": []map[string]any{{"ID": "p1", "Name": "站点A"}},
			"Pages": []map[string]any{
				{"ID": "pg1", "ProjectID": "p1", "Kind": "home", "DraftPath": "/demo", "ActivePath": "/site/demo/", "Publications": []map[string]any{{"Lang": "zh-CN", "ActivePath": "/site/demo/"}}, "Active": true, "Staged": true, "Stale": true, "Version": int64(3), "UpdatedAt": now},
				{"ID": "pg2", "ProjectID": "p1", "Kind": "home", "DraftPath": "/draft", "Active": false, "Staged": false, "Stale": false, "Version": int64(1), "UpdatedAt": now},
			},
		})
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页渲染失败: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"/demo", "/draft", "已发布", "有更新未发布", "草稿", "/workbench?id=pg1", "/site/demo/", "新建站点工程", "新建页面", "站点A"} {
		if !strings.Contains(body, want) {
			t.Fatalf("列表页缺少 %q", want)
		}
	}
}
