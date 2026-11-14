package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	pagecontract "go_wp/internal/module/page/contract"
	projectdto "go_wp/internal/module/project/dto"
	projecthttp "go_wp/internal/module/project/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"go_wp/public/test/support"
)

// themePagesStub 只实现保存主题时真正会被调用的三条页面刷新能力。
//
// 其余方法由嵌入的接口提供（未调用即 nil panic），从而不必为一条落库判据
// 伪造整个 PageService。页面刷新本身是另一条业务链，本用例只证明
// 「主题设置经生产处理器写进数据库并可回读」。
type themePagesStub struct {
	pagecontract.PageService
	refreshedTheme string
}

func (s *themePagesStub) RefreshThemeForTheme(_ context.Context, themeID string, _ json.RawMessage) error {
	s.refreshedTheme = themeID
	return nil
}

func (s *themePagesStub) RefreshStructureForTheme(context.Context, string, json.RawMessage) error {
	return nil
}

func (s *themePagesStub) MarkStaleForTheme(context.Context, string) ([]string, error) {
	return nil, nil
}

// themeBlocksStub 只实现主题设置页读取块候选的那一条查询。
type themeBlocksStub struct {
	blockcontract.BlockService
}

func (themeBlocksStub) List(context.Context, *blockdto.ListReq) ([]blockdto.BlockResp, error) {
	return nil, nil
}

// TestThemeSettingsSavePersistsThroughProductionHandler 证明工作台主题保存的真实落库：
// 生产 SaveThemeSettings 处理器 + 真实 project service + 迁移 PG，保存后回读数据库。
func TestThemeSettingsSavePersistsThroughProductionHandler(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	svc := projectservice.NewService(projectmodel.NewProjectModel(db))
	ctx := context.Background()

	project, err := svc.Create(ctx, &projectdto.CreateReq{Name: "官网"})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}
	theme, err := svc.GetActiveTheme(ctx, project.ID)
	if err != nil || theme == nil {
		t.Fatalf("新工程应有激活主题: %v", err)
	}

	pages := &themePagesStub{}
	h := projecthttp.NewThemeAdminHandle(svc, pages, themeBlocksStub{})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.POST("/admin/themes/settings/save", pageview.HTMLFragment, h.SaveThemeSettings)

	form := url.Values{
		"id":             {theme.ID},
		"colors.primary": {"#0f766e"},
		"colors.text":    {"#111827"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/themes/settings/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存应成功（200），实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 真实落库：从数据库回读，而不是看响应。
	got, err := svc.GetTheme(ctx, theme.ID)
	if err != nil {
		t.Fatalf("回读主题失败: %v", err)
	}
	raw := string(got.Settings)
	if !strings.Contains(raw, "#0f766e") || !strings.Contains(raw, "#111827") {
		t.Fatalf("主题设置未落库: %s", raw)
	}
	if pages.refreshedTheme != theme.ID {
		t.Fatalf("保存后应刷新该主题页面，实际 %q", pages.refreshedTheme)
	}
}
