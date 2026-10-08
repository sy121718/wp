package feature

// site_settings_stale_pg_test.go — 站点设置保存的 stale 标记（FIX-21，真库端到端）。
//
// 放在 public/test（feature 层）而不是 internal/module/project 下：本用例需要**真实**的
// page 服务来跑 stale 网，而 internal/module 下禁止跨模块 import 其他模块的 model/service
// （internal/architecture 的 TestNoCrossModuleServiceModelImport）。feature 层是允许的
// —— 边界约束针对的是生产代码的分层，不是「集成测试只能用 contract」。
//
// 走真实的 SaveSiteSettings（不是直调内部函数）+ 真实的 page 服务与 stale 网：
//   ① 只改 headScripts → 页面被标 stale；
//   ② 内容未变再保存一次 → **不**标（否则保存按钮等于一次全量重建）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projecthttp "go_wp/internal/module/project/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

func TestSaveSiteSettingsMarksStaleOnlyWhenChanged(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "站点设置 stale 用例"})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}
	pageID := "70000000-0000-0000-0000-0000000000a1"
	if err := db.Exec(
		"INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, create_time, update_time) "+
			"VALUES (?, ?, 'home', 'none', '/', '{}'::jsonb, 1, now(), now())", pageID, project.ID).Error; err != nil {
		t.Fatalf("准备页面失败: %v", err)
	}
	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	h := projecthttp.NewSiteSettingsAdminHandle(projects, pages, nil)

	staleOf := func() bool {
		var stale bool
		if qerr := db.Raw("SELECT stale FROM pages WHERE id = ?", pageID).Scan(&stale).Error; qerr != nil {
			t.Fatalf("读 stale 失败: %v", qerr)
		}
		return stale
	}
	save := func(headScripts string) {
		t.Helper()
		form := url.Values{
			"projectId":   {project.ID},
			"name":        {project.Name},
			"headScripts": {headScripts},
		}
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		// 写动作的结论由 shell.RenderJump 渲染整页提示（HTTP 200），需要真实模板渲染器。
		engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
		engine.POST("/admin/settings/save", h.SaveSiteSettings)
		req := httptest.NewRequest(http.MethodPost, "/admin/settings/save", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
			t.Fatalf("保存应 200 渲染成功提示页，实际 %d（body=%s）", rec.Code, rec.Body.String())
		}
	}

	// ① 改了 headScripts → 标 stale。
	save(`<script>console.log("ga")</script>`)
	if !staleOf() {
		t.Fatal("改 headScripts 后页面应被标 stale（否则线上一个字节都不变）")
	}
	t.Log("① 改 headScripts → 页面已标 stale")

	// 复位标记，再用**完全相同**的内容保存一次 → 不该重新标。
	if err := db.Exec("UPDATE pages SET stale = false WHERE id = ?", pageID).Error; err != nil {
		t.Fatalf("复位 stale 失败: %v", err)
	}
	save(`<script>console.log("ga")</script>`)
	if staleOf() {
		t.Fatal("内容未变时不该标 stale（会把保存变成一次全站重建）")
	}
	t.Log("② 内容未变再保存 → 未标记（只点了保存的行为不变）")
}
