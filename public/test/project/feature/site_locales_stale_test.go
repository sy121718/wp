package feature

import (
	pageview "go_wp/pkg/view"
	"go_wp/internal/adminui"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projecthttp "go_wp/internal/module/project/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func newLocaleStaleEnv(t *testing.T) (*gin.Engine, *projectservice.Service, *gorm.DB, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	gin.SetMode(gin.TestMode)
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(t.Context(), &projectdto.CreateReq{Name: "语言清单待重建站点"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	handle := projecthttp.NewSiteSettingsAdminHandle(projects, pages, nil)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/settings", pageview.Slot(projecthttp.SiteSettingsPageTemplate, adminui.RenderPage), handle.SiteSettings)
	router.POST("/admin/settings/locales/save", pageview.Slot(projecthttp.SiteSettingsPageTemplate, adminui.RenderPage), handle.SaveSiteLocales)
	return router, projects, db, project.ID
}

func insertStalePage(t *testing.T, db *gorm.DB, projectID, id string, deleted bool) {
	t.Helper()
	var deletedAt any
	if deleted {
		deletedAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	if err := db.Exec(
		`INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, deleted_at, create_time, update_time)
		 VALUES (?, ?, 'home', 'none', ?, '{}'::jsonb, 1, false, ?, ?, ?)`,
		id, projectID, "pages/"+id+"/draft.json", deletedAt, now, now,
	).Error; err != nil {
		t.Fatalf("插入页面失败: %v", err)
	}
}

type pageStaleRow struct {
	Stale     bool
	UpdatedAt time.Time
}

func pageStaleOf(t *testing.T, db *gorm.DB, id string) pageStaleRow {
	t.Helper()
	var row pageStaleRow
	if err := db.Raw("SELECT stale, update_time FROM pages WHERE id = ?", id).Scan(&row).Error; err != nil {
		t.Fatalf("读取页面 stale 失败: %v", err)
	}
	return row
}

func seedLocales(t *testing.T, svc *projectservice.Service, projectID string) {
	t.Helper()
	if _, err := svc.SaveLocales(t.Context(), &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}, {Lang: "en-US"}},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}
}

func TestSaveSiteLocalesMarksStaleOnChange(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, projectID, "11111111-1111-1111-1111-111111111111", false)
	insertStalePage(t, db, projectID, "22222222-2222-2222-2222-222222222222", true)

	saved := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh-CN", "en-US", "ja"},
		"defaultIndex": {"0"},
		"enabledIndex": {"0", "1"},
	})
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("保存成功应 200 渲染成功提示页，实际 %d：%s", saved.Code, saved.Body.String())
	}
	live := pageStaleOf(t, db, "11111111-1111-1111-1111-111111111111")
	if !live.Stale {
		t.Fatalf("清单变化后未删除页面应 stale=true，实际 %+v", live)
	}
	if gone := pageStaleOf(t, db, "22222222-2222-2222-2222-222222222222"); gone.Stale {
		t.Fatalf("软删页面不应被标记，实际 %+v", gone)
	}
}

func TestSaveSiteLocalesMarksStaleOnDefaultChange(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, projectID, "33333333-3333-3333-3333-333333333333", false)

	saved := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh-CN", "en-US"},
		"defaultIndex": {"1"},
		"enabledIndex": {"0", "1"},
	})
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("保存成功应 200 渲染成功提示页，实际 %d：%s", saved.Code, saved.Body.String())
	}
	if row := pageStaleOf(t, db, "33333333-3333-3333-3333-333333333333"); !row.Stale {
		t.Fatalf("默认语言变化后页面应 stale=true，实际 %+v", row)
	}
}

func TestSaveSiteLocalesSkipsStaleWhenUnchanged(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, projectID, "44444444-4444-4444-4444-444444444444", false)
	before := pageStaleOf(t, db, "44444444-4444-4444-4444-444444444444")

	saved := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh-CN", "en-US"},
		"defaultIndex": {"0"},
		"enabledIndex": {"0", "1"},
	})
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("保存成功应 200 渲染成功提示页，实际 %d：%s", saved.Code, saved.Body.String())
	}
	after := pageStaleOf(t, db, "44444444-4444-4444-4444-444444444444")
	if after.Stale {
		t.Fatalf("清单未变不应触发全站重建，实际 %+v", after)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("清单未变不应写 pages.update_time：before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestSaveSiteLocalesInvalidSkipsStale(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, projectID, "55555555-5555-5555-5555-555555555555", false)

	recorder := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh CN"},
		"defaultIndex": {"0"},
		"enabledIndex": {"0"},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("校验失败应回渲染设置页（200），实际 %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "语言清单不合法") {
		t.Fatalf("缺少校验失败提示：%s", recorder.Body.String())
	}
	if row := pageStaleOf(t, db, "55555555-5555-5555-5555-555555555555"); row.Stale {
		t.Fatalf("校验失败不应触发全站重建，实际 %+v", row)
	}
	rows, err := projects.ListLocales(t.Context(), projectID)
	if err != nil {
		t.Fatalf("读取语言清单失败: %v", err)
	}
	if len(rows) != 2 || rows[0].Lang != "zh-CN" {
		t.Fatalf("校验失败不应落库，实际 %+v", rows)
	}
}
