package feature

import (
	"fmt"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	blockHeaderDocForTr = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"hb1","type":"core.button","props":{"text":"页眉按钮","action":"internal","value":"/a"}}]}`
	blockPromoDocForTr  = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pb1","type":"core.text","props":{"text":"促销正文"}}]}`
)

func newBlockTranslationEnv(t *testing.T) (*gin.Engine, *gorm.DB, string, string, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	gin.SetMode(gin.TestMode)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, "", "", ""
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(t.Context(), &projectdto.CreateReq{Name: "块内文本工作台站点"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	if _, err := projects.SaveLocales(t.Context(), &projectdto.LocalesSaveReq{
		ProjectID: project.ID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}, {Lang: "en-US"}},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	header, err := blocks.Create(t.Context(), &blockdto.CreateReq{
		ProjectID: project.ID, Name: "站点页眉", Kind: "header", Document: []byte(blockHeaderDocForTr),
	})
	if err != nil {
		t.Fatalf("创建页眉块失败: %v", err)
	}
	promo, err := blocks.Create(t.Context(), &blockdto.CreateReq{
		ProjectID: project.ID, Name: "促销块", Kind: "block", Document: []byte(blockPromoDocForTr),
	})
	if err != nil {
		t.Fatalf("创建促销块失败: %v", err)
	}
	pageID := "dddddddd-0000-0000-0000-00000000000d"
	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"},"structure":{"headerBlockId":"%s"}},"root":[{"id":"hd1","type":"core.heading","props":{"text":"本页标题"}},{"id":"ref1","type":"core.globalref","props":{"blockId":"%s"}}]}`,
		header.ID, promo.ID)
	insertTranslationPage(t, db, pageID, project.ID, "/block-tr", doc)

	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	handle := pagehttp.NewPagesAdminHandle(pages, projects, blocks, nil)
	handle.SetContentTranslationStore(i18n.NewContentWriter(db))

	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.GET("/admin/pages/translations", pageview.Slot(pagehttp.PageTranslationsTemplate, adminui.RenderPage), handle.PageTranslations)
	router.POST("/api/page/translations/save", pageview.HTMLFragment, handle.SavePageTranslations)
	return router, db, project.ID, pageID, header.ID
}

func TestPageTranslationsListsBlockText(t *testing.T) {
	router, db, projectID, pageID, headerID := newBlockTranslationEnv(t)

	insertTranslationPage(t, db, "eeeeeeee-0000-0000-0000-00000000000e", projectID, "/other-block",
		fmt.Sprintf(`{"settings":{"layout":{"mode":"full"},"structure":{"headerBlockId":"%s"}},"root":[]}`, headerID))

	body := getTranslationPage(t, router, pageID, "en-US")

	for _, want := range []string{

		`name="rowSource" value="本页标题"`,
		`name="rowSource" value="页眉按钮"`,
		`name="rowSource" value="促销正文"`,

		`>页眉块<`,
		`>全局块<`,

		`本页完成度 0 / 3`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("工作台缺少 %q\n%s", want, body)
		}
	}

	if !strings.Contains(body, "还用在另外 1 个页面") {
		t.Fatalf("块内文本应带跨页面复用提示\n%s", body)
	}
}

func TestSaveBlockTextTranslation(t *testing.T) {
	router, db, _, pageID, _ := newBlockTranslationEnv(t)
	saved := postForm(t, router, "/api/page/translations/save", url.Values{
		"pageId":     {pageID},
		"lang":       {"en-US"},
		"rowContext": {"core.button.text"},
		"rowSource":  {"页眉按钮"},
		"rowHash":    {i18n.ContentHash("页眉按钮")},
		"rowTarget":  {"Header button"},
	})
	if saved.Code != http.StatusOK {
		t.Fatalf("保存块内译文应渲染提示页（200），实际 %d：%s", saved.Code, saved.Body.String())
	}
	if body := saved.Body.String(); !strings.Contains(body, "已保存 1 条译文") {
		t.Fatalf("应写入 1 条译文，实际提示 %s", body)
	}
	target, engine := translationTargetOf(t, db, "页眉按钮", "core.button.text", "en-US")
	if target != "Header button" || engine != i18n.ContentEngineManual {
		t.Fatalf("块内译文未按预期落库: %q / %q", target, engine)
	}
	if !pageStale(t, db, pageID) {
		t.Fatal("块内译文变更应触发全站标记待重建")
	}
	body := getTranslationPage(t, router, pageID, "en-US")
	for _, want := range []string{"Header button", "已翻译", "本页完成度 1 / 3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("保存后工作台缺少 %q\n%s", want, body)
		}
	}
}

func postForm(t *testing.T, router *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	return recorder
}
