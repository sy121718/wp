package feature

import (
	pageview "go_wp/pkg/view"
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

		engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
		engine.POST("/admin/settings/save", pageview.Slot(projecthttp.SiteSettingsPageTemplate, pageview.RenderPage), h.SaveSiteSettings)
		req := httptest.NewRequest(http.MethodPost, "/admin/settings/save", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
			t.Fatalf("保存应 200 渲染成功提示页，实际 %d（body=%s）", rec.Code, rec.Body.String())
		}
	}

	save(`<script>console.log("ga")</script>`)
	if !staleOf() {
		t.Fatal("改 headScripts 后页面应被标 stale（否则线上一个字节都不变）")
	}
	t.Log("① 改 headScripts → 页面已标 stale")

	if err := db.Exec("UPDATE pages SET stale = false WHERE id = ?", pageID).Error; err != nil {
		t.Fatalf("复位 stale 失败: %v", err)
	}
	save(`<script>console.log("ga")</script>`)
	if staleOf() {
		t.Fatal("内容未变时不该标 stale（会把保存变成一次全站重建）")
	}
	t.Log("② 内容未变再保存 → 未标记（只点了保存的行为不变）")
}
