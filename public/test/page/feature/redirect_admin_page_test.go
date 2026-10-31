package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	pagedto "go_wp/internal/module/page/dto"
	pagehttp "go_wp/internal/module/page/inbound/http"
	"go_wp/internal/templates"
)

func TestRedirectAdminPageRenders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, svc, projectID := newPageService(t)
	ctx := context.Background()

	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/handbook", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if _, err = svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/docs", TargetPath: "/handbook",
	}); err != nil {
		t.Fatalf("新增重定向失败: %v", err)
	}

	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	engine.GET("/api/page/redirect", adminui.PageSlot(pagehttp.RedirectPageTemplate), pagehttp.NewHandle(svc).RedirectPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/page/redirect?project="+projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"重定向管理", "当前重定向", "新增重定向",
		"/docs", "/handbook",
		"api/page/redirect/create", "api/page/redirect/delete",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Error("页面在 </html> 之前中断（模板读到缺失键或语法错误）")
	}
}
