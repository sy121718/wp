package feature

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"go_wp/internal/adminui"
	pageenums "go_wp/internal/module/page/enums"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/test/support"
)

// TestBulkDeleteIsAllOrNothing 锁住批量删除的整批语义：
// 混入一个不存在的 id 时整批拒绝，已存在的页面一条都不能被删。
func TestBulkDeleteIsAllOrNothing(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	projectID := uuid.NewString()
	if err := db.Exec("INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, '批量删除用例', '{}'::jsonb, now(), now())", projectID).Error; err != nil {
		t.Fatalf("准备工程失败：%v", err)
	}

	ids := make([]string, 0, 3)
	for i := 0; i < 2; i++ {
		id := uuid.NewString()
		ids = append(ids, id)
		if err := db.Exec(
			"INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, create_time, update_time) "+
				"VALUES (?, ?, 'home', 'none', ?, '{}'::jsonb, 1, now(), now())", id, projectID, "/p"+id[:8]).Error; err != nil {
			t.Fatalf("准备页面失败：%v", err)
		}
	}
	// 第三个目标不存在：整批必须拒绝。
	ids = append(ids, uuid.NewString())

	proj := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, proj, nil, nil, nil, nil, nil)
	h := pagehttp.NewPagesAdminHandle(svc, proj, nil, nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/admin/pages/bulk-delete", pageview.HTMLFragment, h.PagesBulkDelete)

	rec := postPageForm(engine, "/admin/pages/bulk-delete", url.Values{"ids": ids})
	if rec.Code < http.StatusBadRequest {
		t.Fatalf("混入缺失目标应整批拒绝，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data-ui-error") {
		t.Fatalf("失败应渲染错误区：%s", rec.Body.String())
	}
	for _, id := range ids[:2] {
		var alive int64
		if err := db.Table("pages").Where("id = ? AND deleted_at IS NULL", id).Count(&alive).Error; err != nil {
			t.Fatalf("复核页面状态失败：%v", err)
		}
		if alive != 1 {
			t.Fatalf("整批被拒时页面 %s 必须仍在（不能部分删除）", id)
		}
	}

	// 全部合法：一次整批删除，成功走共享提示组件。
	rec = postPageForm(engine, "/admin/pages/bulk-delete", url.Values{"ids": ids[:2]})
	if rec.Code != http.StatusOK {
		t.Fatalf("全部合法应 200，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("成功应走公共成功出口：%s", rec.Body.String())
	}
	for _, id := range ids[:2] {
		var alive int64
		if err := db.Table("pages").Where("id = ? AND deleted_at IS NULL", id).Count(&alive).Error; err != nil {
			t.Fatalf("复核页面状态失败：%v", err)
		}
		if alive != 0 {
			t.Fatalf("批量删除后页面 %s 应已删除", id)
		}
	}

	// 已删除的 id 再次删除：整批拒绝为「页面不存在」。
	rec = postPageForm(engine, "/admin/pages/bulk-delete", url.Values{"ids": ids[:1]})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("重复删除应为 404，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), pageenums.ErrPageNotFound) &&
		!strings.Contains(rec.Body.String(), "data-ui-error") {
		t.Fatalf("重复删除应渲染失败片段：%s", rec.Body.String())
	}
}
