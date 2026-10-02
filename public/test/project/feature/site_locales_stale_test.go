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
//  3. 清单内容未变（原样再保存一次）→ 不触发（stale 与 update_time 均不变）；
//  4. 校验失败 → 不落库也不触发。

import (
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

// newLocaleStaleEnv 装配「语言清单保存 → 全站待重建」测试环境：
// 真实 PG（隔离 schema，跑生产迁移建表）+ project 契约 + page 契约
// （仅 model 参与，其余依赖传 nil）。
//
// pages 行只补 MarkStaleForI18n 与页面外键所需的真实列：本用例验证的是
// 「保存成功后编排是否发生」，不涉及页面其余业务字段与构建链路。
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
	router.GET("/admin/settings", handle.SiteSettings)
	router.POST("/admin/settings/locales/save", handle.SaveSiteLocales)
	return router, projects, db, project.ID
}

// insertStalePage 插入一条页面行（stale 初始为 false；deleted 为 true 时置软删时间）。
//
// 真实 pages 表要求 project_id 外键、kind/content_target_type 满足
// pages_content_contract_check（home + none）、draft_path/draft_document/draft_version
// 非空，故按生产列结构补全。
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

// pageStaleRow 页面可观测状态：stale 与 update_time（用于断言是否被标记）。
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
	insertStalePage(t, db, projectID, "11111111-1111-1111-1111-111111111111", false)
	insertStalePage(t, db, projectID, "22222222-2222-2222-2222-222222222222", true)

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
	insertStalePage(t, db, projectID, "33333333-3333-3333-3333-333333333333", false)

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
	insertStalePage(t, db, projectID, "44444444-4444-4444-4444-444444444444", false)
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
		t.Fatalf("清单未变不应写 pages.update_time：before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}

// TestSaveSiteLocalesInvalidSkipsStale 校验失败 → 不落库也不触发。
func TestSaveSiteLocalesInvalidSkipsStale(t *testing.T) {
	router, projects, db, projectID := newLocaleStaleEnv(t)
	seedLocales(t, projects, projectID)
	insertStalePage(t, db, projectID, "55555555-5555-5555-5555-555555555555", false)

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
