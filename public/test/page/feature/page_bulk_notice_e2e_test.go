package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	pagehttp "go_wp/internal/module/page/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

func TestBulkDeleteNoticeCarriesCounts(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	projectID := uuid.NewString()
	if err := db.Exec("INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, '批量回执用例', '{}'::jsonb, now(), now())", projectID).Error; err != nil {
		t.Fatalf("准备工程失败：%v", err)
	}

	var ids []string
	for i := 0; i < 2; i++ {
		id := uuid.NewString()
		ids = append(ids, id)
		if err := db.Exec(
			"INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, create_time, update_time) "+
				"VALUES (?, ?, 'home', 'none', ?, '{}'::jsonb, 1, now(), now())", id, projectID, "/p"+id[:8]).Error; err != nil {
			t.Fatalf("准备页面失败：%v", err)
		}
	}
	ids = append(ids, uuid.NewString())

	proj := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, proj, nil, nil, nil, nil, nil)
	h := pagehttp.NewPagesAdminHandle(svc, proj, nil, nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.POST("/admin/pages/bulk-delete", h.PagesBulkDelete)

	form := url.Values{"ids": ids}
	req := httptest.NewRequest(http.MethodPost, "/admin/pages/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("批量删除应渲染提示页（200），实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf(`有跳过应走失败提示页（data-jump-state="err"）：%s`, body)
	}
	for _, want := range []string{"已删除 2 个", "1 个未能删除"} {
		if !strings.Contains(body, want) {
			t.Fatalf("提示页缺少计数文案 %q（body 片段：%s）", want, firstN(body, 600))
		}
	}
	t.Log("提示页渲染：含「已删除 2 个，1 个未能删除」")
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
