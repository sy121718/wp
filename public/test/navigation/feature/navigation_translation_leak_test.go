package feature

import (
	"context"
	"fmt"
	"go_wp/internal/adminui"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navigationservice "go_wp/internal/module/navigation/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

var translationLeakTokens = []string{
	"SQLSTATE", "42P01", "sys_translation", `relation "`, "uq_", "pg_", "constraint",
}

func translationWriteRequest(projectID, lang, sourceText, target string) *http.Request {
	body := fmt.Sprintf(
		`{"action":"translations","project":%q,"lang":%q,"rows":[{"context":%q,"hash":%q,"target":%q}]}`,
		projectID, lang, i18n.ContentContext("navigation", "label"), i18n.ContentHash(sourceText), target)
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/translations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestNavigationTranslationSaveHidesInternalError(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	if err := db.Exec("ALTER TABLE sys_translation RENAME TO sys_translation_leak_probe").Error; err != nil {
		t.Fatalf("制造写入失败场景失败: %v", err)
	}

	writer := i18n.NewContentWriter(db)
	_, uerr := writer.Upsert(context.Background(), []i18n.ContentWriteItem{{
		SourceHash: i18n.ContentHash("首页"), Context: i18n.ContentContext("navigation", "label"),
		Lang: "en-US", SourceText: "首页", TargetText: "Home", Engine: i18n.ContentEngineManual,
	}})
	if uerr == nil || !strings.Contains(uerr.Error(), "sys_translation") {
		t.Fatalf("夹具不符：写入失败的原始错误应带表名，实际 %v", uerr)
	}
	t.Logf("原始错误（只应进日志）: %v", uerr)

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))

	handle := navigationhttp.NewHandle(navSvc)
	wireNavigationTranslationWriter(t, db, handle, navSvc.(*navigationservice.Service), projects)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	router.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.POST("/api/navigation/translations", pageview.HTMLFragment, handle.UpdateTranslations)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, translationWriteRequest(projectID, "en-US", "首页", "Home"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("写入失败应 500（归口），实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}
	payload := recorder.Body.String()

	if !strings.Contains(payload, "data-ui-error") {
		t.Fatalf("失败分支应渲染错误片段，实际 body=%s", payload)
	}
	for _, tok := range translationLeakTokens {
		if strings.Contains(payload, tok) {
			t.Errorf("响应泄漏内部细节 %q: %s", tok, payload)
		}
	}

	if strings.Contains(payload, "%!") || strings.Contains(payload, "{detail}") {
		t.Errorf("归口句出现格式化垃圾（占位符没按语言填）: %s", payload)
	}
}

func TestNavigationTranslationSaveMessageIsLocalizedWithoutFormatGarbage(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := navigationhttp.NewHandle(navSvc)
	wireNavigationTranslationWriter(t, db, handle, navSvc.(*navigationservice.Service), projects)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	router.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.POST("/api/navigation/translations", pageview.HTMLFragment, handle.UpdateTranslations)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, translationWriteRequest(projectID, "en-US", "首页", "Home"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("保存应 200，实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}
	payload := recorder.Body.String()
	// 回执是 Jet 片段：写入条数来自写服务，句子在模板里。
	if !strings.Contains(payload, `role="status"`) || !strings.Contains(payload, "1") {
		t.Fatalf("成功响应应是带写入条数的结果片段，实际 %s", payload)
	}
	if strings.Contains(payload, "%!") {
		t.Errorf("成功句出现格式化垃圾（占位符没按语言填）: %s", payload)
	}

	var target string
	row := db.Raw("SELECT target_text FROM sys_translation WHERE source_hash = ? AND lang = 'en-US'",
		i18n.ContentHash("首页")).Scan(&target)
	if row.Error != nil || target != "Home" {
		t.Fatalf("译文未落库: err=%v target=%q", row.Error, target)
	}
}

func translationWriteRaw(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/translations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func translationRowCount(t *testing.T, db *gorm.DB, sourceText, lang string) int64 {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT count(*) FROM sys_translation WHERE source_hash = ? AND lang = ?",
		i18n.ContentHash(sourceText), lang).Scan(&count).Error; err != nil {
		t.Fatalf("查询译文行失败: %v", err)
	}
	return count
}

func TestNavigationTranslationRowKeysAreRequired(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := navigationhttp.NewHandle(navSvc)
	wireNavigationTranslationWriter(t, db, handle, navSvc.(*navigationservice.Service), projects)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	router.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.POST("/api/navigation/translations", pageview.HTMLFragment, handle.UpdateTranslations)

	context := i18n.ContentContext("navigation", "label")
	hash := i18n.ContentHash("首页")
	wrap := func(rows string) string {
		return fmt.Sprintf(`{"action":"translations","project":%q,"lang":"en-US","rows":%s}`, projectID, rows)
	}

	staleBase := menuStaleJobs(t, db, projectID)

	for _, tc := range []struct{ name, rows string }{
		{"缺 target", fmt.Sprintf(`[{"context":%q,"hash":%q}]`, context, hash)},
		{"缺 context", fmt.Sprintf(`[{"hash":%q,"target":"Home"}]`, hash)},
		{"缺 hash", fmt.Sprintf(`[{"context":%q,"target":"Home"}]`, context)},
		{"第二行缺 target", fmt.Sprintf(`[{"context":%q,"hash":%q,"target":"Home"},{"context":%q,"hash":"h-2"}]`,
			context, hash, context)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := translationWriteRaw(t, router, wrap(tc.rows))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("缺键的行应 400（否则会被当成空译文静默跳过），实际 %d body=%s",
					recorder.Code, recorder.Body.String())
			}
			if got := translationRowCount(t, db, "首页", "en-US"); got != 0 {
				t.Fatalf("被拒的请求不该写库，但已有 %d 行译文", got)
			}
			if n := menuStaleJobs(t, db, projectID); n != staleBase {
				t.Fatalf("被拒的请求不该入队任何重建任务，基线 %d 条，实际 %d 条", staleBase, n)
			}
		})
	}

	empty := translationWriteRaw(t, router, wrap(fmt.Sprintf(`[{"context":%q,"hash":%q,"target":""}]`, context, hash)))
	if empty.Code != http.StatusOK {
		t.Fatalf("显式空 target 应被接受（本次不改动），实际 %d body=%s", empty.Code, empty.Body.String())
	}
	// 回执是 Jet 片段：零写入时片段里必须出现 0。
	if !strings.Contains(empty.Body.String(), `role="status"`) || !strings.Contains(empty.Body.String(), "0") {
		t.Fatalf("空译文不该产生写入，实际 %s", empty.Body.String())
	}
	if got := translationRowCount(t, db, "首页", "en-US"); got != 0 {
		t.Fatalf("空译文不该写库，但已有 %d 行", got)
	}
	if n := menuStaleJobs(t, db, projectID); n != staleBase {
		t.Fatalf("没有真正变更时不该入队任何重建任务：基线 %d 条，实际 %d 条", staleBase, n)
	}

	written := translationWriteRaw(t, router, wrap(fmt.Sprintf(`[{"context":%q,"hash":%q,"target":"Home"}]`, context, hash)))
	if written.Code != http.StatusOK {
		t.Fatalf("合法写入应 200，实际 %d body=%s", written.Code, written.Body.String())
	}
	if got := translationRowCount(t, db, "首页", "en-US"); got != 1 {
		t.Fatalf("合法写入应落 1 行译文，实际 %d", got)
	}

	if n := menuStaleJobs(t, db, projectID); n == 0 {
		t.Fatal("真正变更后必须为受影响位置可靠入队 menu_stale 任务")
	}
}
