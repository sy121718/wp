package feature

// redirect_admin_page_test.go — 重定向管理页的渲染冒烟（审计 SEO-025）。
//
// 钉住两件事：
//   - admin/page_redirects.html 的 Jet 语法与数据契约成立（模板写错只会在用户点开时崩）；
//   - 页面把服务端给的重定向真的渲染出来（不是「接口对了、页面空白」）。
//
// 末尾断言 </html> 存在：Jet 读到缺失的键会在那一行**静默中断**（HTTP 仍是 200，
// 中断点之后的内容整块消失），只断言状态码会漏掉这类故障。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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
	// 模板根相对包目录：public/test/page/feature → 项目根/internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	engine.GET("/api/page/redirect", pagehttp.NewHandle(svc).RedirectPage)

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
