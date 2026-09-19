package feature

// navigation_translation_leak_test.go — 导航译文工作台的写入失败不得把数据库原文渲进页面。
//
// 缺口原状：navigation_translation_handle.go 的保存分支是
// `data.Errors = []string{"保存失败：" + uerr.Error()}`—— 泄漏的是**页面正文**
// 而不是 JSON 响应体，而 scripts/check-no-internal-error-leak.sh 的判据只看
// response.ErrorWithMessage / c.String 的实参，所以这条一直绿着、泄漏一直在。
//
// 本用例从真实失败形态出发：把 sys_translation 改名，让 Upsert 撞上 PG 的
// `relation "sys_translation" does not exist (SQLSTATE 42P01)`——
// 正是「表名 + SQLSTATE」那一类内部细节。断言是**强**的：页面正文出现
// SQLSTATE / 42P01 / sys_translation / relation " / uq_ / pg_ / constraint 任一即失败。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

// translationLeakTokens 页面正文里的内部细节指纹。
//
// 与 navigation_error_leak_test.go 的 JSON 判据分开：那里能拿 "navigations" / "uuid"
// 当指纹，而页面正文本身就满是 /admin/navigations/... 的链接，不能进这份清单。
var translationLeakTokens = []string{
	"SQLSTATE", "42P01", "sys_translation", `relation "`, "uq_", "pg_", "constraint",
}

// TestNavigationTranslationSaveHidesInternalError 写入失败：页面给归口文案，不带数据库原文。
func TestNavigationTranslationSaveHidesInternalError(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	// 真实失败形态：目标表不存在。PG 的原文里同时带表名与 SQLSTATE 码。
	if err := db.Exec("ALTER TABLE sys_translation RENAME TO sys_translation_leak_probe").Error; err != nil {
		t.Fatalf("制造写入失败场景失败: %v", err)
	}

	// 反证：这条失败路径的**原始错误**确实带表名与 SQLSTATE。
	// 少了这一步，「页面没泄漏」可能只是「错误里本来就没有内部细节」—— 断言等于没断言。
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
	handle := navigationhttp.NewNavigationTranslationHandle(navSvc, projects)
	handle.SetContentWriter(writer)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.POST("/admin/navigations/translations/save", handle.SaveNavigationTranslations)

	form := url.Values{}
	form.Set("project", projectID)
	form.Set("lang", "en-US")
	form.Add("rowContext", i18n.ContentContext("navigation", "label"))
	form.Add("rowHash", i18n.ContentHash("首页"))
	form.Add("rowTarget", "Home")
	req := httptest.NewRequest(http.MethodPost, "/admin/navigations/translations/save",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("保存失败应原地重渲页面（200），实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	// 先证明真的走到了失败分支：没有这句断言，一条「什么都没渲染」的空页也能通过下面的检查。
	if !strings.Contains(body, "保存失败") {
		t.Fatalf("失败分支应给出提示条，实际 body=%s", body)
	}
	for _, tok := range translationLeakTokens {
		if strings.Contains(body, tok) {
			t.Errorf("页面正文泄漏内部细节 %q: %s", tok, body)
		}
	}
	// 归口文案必须真的落进页面（模板取词后的中文兜底），否则用户只看到「保存失败：」。
	if !strings.Contains(body, "操作失败") {
		t.Errorf("失败提示应带归口文案，实际 body=%s", body)
	}
}
