package feature

// page_bulk_notice_e2e_test.go — 批量删除的结论文案（真库端到端）。
//
// 判据是**操作者能看到什么**：一次「成功 2 / 跳过 1」的批量删除，响应必须是整页提示
// （HTTP 200 + data-jump-state="err"），且文案里两个计数都在 —— 只报「操作完成」会把
// 部分成功静默成全部成功。
//
// 为什么在 feature 层而不是模块内：它要真库 + 装配出 page 与 project 两个模块，
// 而模块内测试不允许跨模块 import service/model（internal/architecture 的
// TestNoCrossModuleServiceModelImport 守着这条），feature 层没有这个限制。

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
	// 两个真实页面（会被删掉 → n=2）+ 一个不存在的 id（跳过 → m=1）。
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
	ids = append(ids, uuid.NewString()) // 不存在 → 跳过

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
	// 有跳过 → 失败档（不自动跳，运营要看清剩下哪些没删掉）。
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
