package feature

// site_locales_stale_test.go — 保存语言清单后自动标记全站待重建
//（多语言 P3，docs/06-D §15.9 遗留第 1 条）。
//
// 背景：语言切换器链接与 hreflang 是构建期写进产物字节的，改清单不重建则前台无变化。
// 本用例走真实链路：gin 路由 → dashboard handler（编排）→ project 契约 SaveLocales
//（真 PG project_locales）+ page 契约 MarkStaleForI18n（真 page model，真
// "UPDATE pages SET stale = true ..."），断言可观测状态。
//
// 覆盖：
//  1. 清单内容变化（增语言）→ 未删除页面 stale 被置位，软删页面不受影响；
//  2. 默认语言切换（构建可见内容变化的一种）→ 触发；
//  3. 清单内容未变（原样再保存一次）→ 不触发（stale 与 updated_at 均不变）；
//  4. 校验失败 → 不落库也不触发。

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// newLocaleStaleEnv 装配「语言清单保存 → 全站待重建」测试环境：
// 真实 PG（隔离 schema）+ project 契约 + page 契约（仅 model 参与，其余依赖传 nil）。
//
// pages 表只建 MarkStaleForI18n 命中的列（stale / deleted_at / updated_at）：
// 本用例验证的是「保存成功后编排是否发生」，不涉及页面其余字段与构建链路。
func newLocaleStaleEnv(t *testing.T) (*gin.Engine, *projectservice.Service, *gorm.DB, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	gin.SetMode(gin.TestMode)
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil, nil, nil, ""
	}
	for _, statement := range []string{
		"CREATE TABLE projects (id UUID PRIMARY KEY, name TEXT NOT NULL, settings JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)",
		"CREATE TABLE project_locales (project_id UUID NOT NULL, lang TEXT NOT NULL, sort_order INTEGER NOT NULL DEFAULT 0, is_default BOOLEAN NOT NULL DEFAULT false, enabled BOOLEAN NOT NULL DEFAULT true, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL, PRIMARY KEY(project_id, lang))",
		"CREATE TABLE page_site_slots (id UUID PRIMARY KEY, project_id UUID NOT NULL, slot TEXT NOT NULL, page_id UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)",
		"CREATE TABLE pages (id UUID PRIMARY KEY, stale BOOLEAN NOT NULL DEFAULT false, deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL)",
	} {
		if err = db.Exec(statement).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(t.Context(), &projectdto.CreateReq{Name: "语言清单待重建站点"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, nil, nil, nil, nil, nil, nil)
	handle := dashboardhttp.NewHandle(pages, projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/settings", handle.SiteSettings)
	router.POST("/admin/settings/locales/save", handle.SaveSiteLocales)
	return router, projects, db, project.ID
}

// insertStalePage 插入一条页面行（stale 初始为 false；deleted 为 true 时置软删时间）。
func insertStalePage(t *testing.T, db *gorm.DB, id string, deleted bool) {
	t.Helper()
	var deletedAt any
	if deleted {
		deletedAt = time.Now().UTC()
	}
	if err := db.Exec(
		"INSERT INTO pages (id, stale, deleted_at, updated_at) VALUES (?, false, ?, ?)",
		id, deletedAt, time.Now().UTC(),
	).Error; err != nil {
		t.Fatalf("插入页面失败: %v", err)
	}
}

// pageStaleRow 页面可观测状态：stale 与 updated_at（用于断言是否被标记）。
type pageStaleRow struct {
	Stale     bool
	UpdatedAt time.Time
}

func pageStaleOf(t *testing.T, db *gorm.DB, id string) pageStaleRow {
	t.Helper()
	var row pageStaleRow
	if err := db.Raw("SELECT stale, updated_at FROM pages WHERE id = ?", id).Scan(&row).Error; err != nil {
		t.Fatalf("读取页面 stale 失败: %v", err)
	}
	return row
}

// seedLocales 初始化语言清单（zh-CN 默认 + en-US）。
func seedLocales(t *testing.T, svc *projectservice.Service, projectID string) {
	t.Helper()
	if _, err := svc.SaveLocales(t.Context(), &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}, {Lang: "en-US"}},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}
}

// TestSaveSiteLocalesMarksStaleOnChange 清单内容变化 → 未删除页面 stale 置位。
func TestSaveSiteLocalesMarksStaleOnChange(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, "11111111-1111-1111-1111-111111111111", false)
	insertStalePage(t, db, "22222222-2222-2222-2222-222222222222", true)

	saved := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh-CN", "en-US", "ja"},
		"defaultIndex": {"0"},
		"enabledIndex": {"0", "1"},
	})
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("保存成功应 303 回跳，实际 %d：%s", saved.Code, saved.Body.String())
	}
	live := pageStaleOf(t, db, "11111111-1111-1111-1111-111111111111")
	if !live.Stale {
		t.Fatalf("清单变化后未删除页面应 stale=true，实际 %+v", live)
	}
	if gone := pageStaleOf(t, db, "22222222-2222-2222-2222-222222222222"); gone.Stale {
		t.Fatalf("软删页面不应被标记，实际 %+v", gone)
	}
}

// TestSaveSiteLocalesMarksStaleOnDefaultChange 默认语言切换（构建可见内容变化）→ 触发。
func TestSaveSiteLocalesMarksStaleOnDefaultChange(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, "33333333-3333-3333-3333-333333333333", false)

	saved := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh-CN", "en-US"},
		"defaultIndex": {"1"}, // 默认语言改为 en-US
		"enabledIndex": {"0", "1"},
	})
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("保存成功应 303 回跳，实际 %d：%s", saved.Code, saved.Body.String())
	}
	if row := pageStaleOf(t, db, "33333333-3333-3333-3333-333333333333"); !row.Stale {
		t.Fatalf("默认语言变化后页面应 stale=true，实际 %+v", row)
	}
}

// TestSaveSiteLocalesSkipsStaleWhenUnchanged 清单内容未变（原样再保存）→ 不触发。
func TestSaveSiteLocalesSkipsStaleWhenUnchanged(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, "44444444-4444-4444-4444-444444444444", false)
	before := pageStaleOf(t, db, "44444444-4444-4444-4444-444444444444")

	saved := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh-CN", "en-US"}, // 与库中一致
		"defaultIndex": {"0"},
		"enabledIndex": {"0", "1"},
	})
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("保存成功应 303 回跳，实际 %d：%s", saved.Code, saved.Body.String())
	}
	after := pageStaleOf(t, db, "44444444-4444-4444-4444-444444444444")
	if after.Stale {
		t.Fatalf("清单未变不应触发全站重建，实际 %+v", after)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("清单未变不应写 pages.updated_at：before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}

// TestSaveSiteLocalesInvalidSkipsStale 校验失败 → 不落库也不触发。
func TestSaveSiteLocalesInvalidSkipsStale(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, "55555555-5555-5555-5555-555555555555", false)

	recorder := postForm(t, router, "/admin/settings/locales/save", url.Values{
		"projectId":    {projectID},
		"langs":        {"zh CN"}, // 非法语言码
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
