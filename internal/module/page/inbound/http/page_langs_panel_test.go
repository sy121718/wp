package pagehttp_test

// page_langs_panel_test.go — 语言产出范围面板的真实渲染（迁移 491）。
//
// **外部测试包**（pagehttp_test）：本包被 internal/routers 依赖，而 support 又 import routers，
// 用内部测试包会成 import cycle。导出面够用（PagesAdminHandle 别名 + 导出构造器 + 导出方法）。
//
// 判据是**渲染结果**而不是「函数被调用」：模板里少一个 `{{if}}` 分支、少一个隐藏
// csrf_token 域、或者键名与 handler 的 gin.H 对不上，都不会编译失败 ——
// 前者让按钮点了没反应（CSRF 拒），后者让整段片段在运行期炸掉（片段 inert，
// 页面上只是少了东西）。所以这里用真实模板引擎渲染并断言关键元素。
//
// 库：support.NewMigratedPGTestDB（生产迁移建的模板库），页面与语言清单都插真实行 ——
// 不手抄 CREATE TABLE。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	pagehttp "go_wp/internal/module/page/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/jsonx"
	"go_wp/public/test/support"
)

// langPanelProjectList 站点工程契约替身：只覆写跨工程扇出真正调用的 List。
//
// 内嵌 nil 接口让其余方法自动满足（调用即 panic）—— 与同包 filterProjectList 同一手法。
//
// 这里给的**不是凑数形参**：page 侧在没有工程上下文问页面归属时会走
// fanoutProjectIDs → List（page_scope.go）。契约缺失虽有一条「直接读 projects 表」的兜底，
// 但那张表有 RLS、无作用域时读回 0 行 → ErrProjectRequired → 面板整块空，而它的表现是
// 「运营点了语言按钮什么都没出来」。所以这是真实依赖，不是装饰。
type langPanelProjectList struct {
	projectcontract.ProjectService
	ids   []string
	langs []string
}

func (f langPanelProjectList) List(context.Context) ([]projectcontract.ProjectResp, error) {
	out := make([]projectcontract.ProjectResp, 0, len(f.ids))
	for _, id := range f.ids {
		out = append(out, projectcontract.ProjectResp{ID: id})
	}
	return out, nil
}

// EnabledLangs 站点启用语言（默认语言在前）。**必须与上面 INSERT 进 project_locales 的两条一致**：
// 面板列的是「本页每种语言能不能产出」，而语言清单是站点级设置、不在这套表里。
// 返回副本：pipeline.EnabledLangs 会就地排序，别把 fixture 的切片改掉。
func (f langPanelProjectList) EnabledLangs(context.Context, string) ([]string, error) {
	return append([]string(nil), f.langs...), nil
}

// DefaultLocale 站点默认语言。契约保证 EnabledLangs 的**默认语言在前**，所以取首项 ——
// 面板要拿它把「默认语言不可排除」那一行的文案与不可操作性渲染出来。
func (f langPanelProjectList) DefaultLocale(context.Context, string) (string, error) {
	if len(f.langs) == 0 {
		return "", nil
	}
	return f.langs[0], nil
}

// Exists 工程是否存在 —— 只在本 fixture 造的工程集里判定，不去猜不存在的 id。
func (f langPanelProjectList) Exists(_ context.Context, id string) (bool, error) {
	for _, candidate := range f.ids {
		if candidate == id {
			return true, nil
		}
	}
	return false, nil
}

// GetActiveTheme 工程激活主题。本用例没有主题，按「没有主题」返回 (nil, nil) ——
// 与同包 filterProjectList 同口径：调用方据此走无主题分支，而不是把它当错误。
func (f langPanelProjectList) GetActiveTheme(context.Context, string) (*projectcontract.ThemeResp, error) {
	return nil, nil
}

// seedLangPanelFixture 一个工程 + 两种启用语言（en-AU 默认 / zh-CN 非默认）+ 一个页面。
func seedLangPanelFixture(t *testing.T) (svc *pageservice.Service, projects projectcontract.ProjectService, pageID, projectID string) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	projectID = "30000000-0000-0000-0000-000000000001"
	support.SeedProjectRow(t, db, projectID, "语言面板用例")
	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) "+
			"VALUES (?, 'en-AU', 0, true, true, now(), now()), (?, 'zh-CN', 1, false, true, now(), now())",
		projectID, projectID).Error; err != nil {
		t.Fatalf("准备语言清单失败：%v", err)
	}
	pageID = "30000000-0000-0000-0000-0000000000a1"
	if err := db.Exec(
		"INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, create_time, update_time) "+
			"VALUES (?, ?, 'home', 'none', '/contact', '{}'::jsonb, 1, now(), now())",
		pageID, projectID).Error; err != nil {
		t.Fatalf("准备页面行失败：%v", err)
	}
	if err := db.Exec(
		"INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, create_time, update_time, excluded_langs) "+
			"VALUES (?, ?, 'home', 'none', '/contact-cn', '{}'::jsonb, 1, now(), now(), ARRAY['zh-CN']::text[])",
		"30000000-0000-0000-0000-0000000000a2", projectID).Error; err != nil {
		t.Fatalf("准备已排除页面行失败：%v", err)
	}
	projects = langPanelProjectList{ids: []string{projectID}, langs: []string{"en-AU", "zh-CN"}}
	return pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil),
		projects, pageID, projectID
}

// TestPageLangsPanelRenders 面板渲染：两种语言各按自己的状态给出不同操作。
// TestPageLangsPanelReturnsJSON 面板回 **JSON 数据**，不回 HTML 片段。
//
// 判据在 2026-10 改过：从前这里断言「片段里有哪些元素（表单 action / CSRF 隐藏域 /
// 文案）」—— 那是 htmx 换 HTML 片段的时代。现在局部刷新走 {code,message,data}：
// 数据是 JSON，DOM 由前端用页面里的 Jet <template> 填充（ui/json_swap.js）。
//
// 所以这一层只钉**数据契约**：有哪几行、每行的状态码是什么。
// 「按钮长什么样、文案是哪句」属于模板，由模板测试与视觉基线守。
func TestPageLangsPanelReturnsJSON(t *testing.T) {
	svc, projects, pageID, _ := seedLangPanelFixture(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := pagehttp.NewPagesAdminHandle(svc, projects, nil, nil)
	router.GET("/admin/page-langs/panel", h.PageLangsPanel)

	decode := func(rec *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var body map[string]any
		if err := jsonx.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("响应不是 JSON：%v\n%s", err, rec.Body.String())
		}
		return body
	}
	rowsOf := func(body map[string]any) []any {
		t.Helper()
		data, _ := body["data"].(map[string]any)
		items, _ := data["items"].([]any)
		return items
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/page-langs/panel?pageId="+pageID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("面板应回 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("面板必须回 JSON（后端不产 HTML 片段），得到 %q", ct)
	}
	body := decode(rec)
	// 本仓两种成功码都出现过：response.Success 用 200，writeJSON 用 0。
	// 扩展（json_swap.isFailure）两个都当成功 —— 这里同样只钉「不是失败档」。
	if code, _ := body["code"].(float64); code != 200 && code != 0 {
		t.Fatalf("成功响应 code 应为 0 或 200，实际 %v", body["code"])
	}
	data, _ := body["data"].(map[string]any)
	if data["pageId"] != pageID {
		t.Errorf("data.pageId 应为 %s，实际 %v", pageID, data["pageId"])
	}

	items := rowsOf(body)
	if len(items) != 2 {
		t.Fatalf("应有两行语言，实际 %d：%s", len(items), rec.Body.String())
	}
	seen := map[string]string{}
	for _, it := range items {
		row, _ := it.(map[string]any)
		lang, _ := row["lang"].(string)
		status, _ := row["status"].(string)
		seen[lang] = status
		// 状态码只能取这三档：模板按它选 data-when 分支。
		if status != "excluded" && status != "published" && status != "draft" {
			t.Errorf("状态码只能是 excluded/published/draft，得到 %q", status)
		}
		// 行里不许有成品文案：取词发生在模板渲染期，不在 JSON 里。
		for _, banned := range []string{"statusText", "noteText", "label"} {
			if _, ok := row[banned]; ok {
				t.Errorf("行里不该有成品文案字段 %q —— 取词发生在模板渲染期", banned)
			}
		}
	}
	if _, ok := seen["en-AU"]; !ok {
		t.Errorf("缺少 en-AU：%v", seen)
	}
	if _, ok := seen["zh-CN"]; !ok {
		t.Errorf("缺少 zh-CN：%v", seen)
	}

	// 另一页把 zh-CN 标成排除：状态码必须是 excluded（模板据此显示「恢复」）。
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet,
		"/admin/page-langs/panel?pageId=30000000-0000-0000-0000-0000000000a2", nil))
	found := false
	for _, it := range rowsOf(decode(rec2)) {
		row, _ := it.(map[string]any)
		if row["lang"] == "zh-CN" {
			found = true
			if row["status"] != "excluded" {
				t.Errorf("已排除语言的状态码应为 excluded，实际 %v", row["status"])
			}
			if row["isExcluded"] != true {
				t.Errorf("isExcluded 应为 true（模板据此选「恢复」分支），实际 %v", row["isExcluded"])
			}
		}
	}
	if !found {
		t.Fatalf("第二页应有 zh-CN 行：%s", rec2.Body.String())
	}
}

// TestPageLangsPanelDegradesOnLoadFailure 页面读不到（id 不存在）时仍渲染完整片段：
// 表头常驻 + 空态整行进 tbody（colspan = 列数）+ 归口文案（不直出内部错误）。
//
// 为什么这条重要：面板是 HTMX 片段，渲染失败时页面上**什么都不显示**（inert 的失败）——
// 运营点了「语言」按钮却没有任何反应，而日志里可能什么都没有。
func TestPageLangsPanelLoadFailureReturnsJSON(t *testing.T) {
	svc, projects, _, _ := seedLangPanelFixture(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := pagehttp.NewPagesAdminHandle(svc, projects, nil, nil)
	router.GET("/admin/page-langs/panel", h.PageLangsPanel)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/page-langs/panel?pageId=30000000-0000-0000-0000-0000000000ff", nil))

	// 失败一律 {code,message}（用户裁定：一个提示就够）。从前这里返回「降级渲染的完整片段」，
	// 那是 HTMX 换 HTML 的时代 —— 现在前端只认 JSON，回 HTML 会让它解析失败。
	var body map[string]any
	if err := jsonx.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("失败响应必须是 JSON：%v\n%s", err, rec.Body.String())
	}
	if rec.Code < 400 {
		t.Fatalf("读不到页面应回 4xx/5xx，实际 %d：%s", rec.Code, rec.Body.String())
	}
	msg, _ := body["message"].(string)
	if msg == "" {
		t.Fatalf("失败响应必须带可读文案：%s", rec.Body.String())
	}
	// 归口（不直出内部错误）：页面不存在是业务错误，给可读文案而不是驱动原文。
	for _, banned := range []string{"SQLSTATE", "record not found", "pq:"} {
		if strings.Contains(msg, banned) {
			t.Fatalf("文案里出现了驱动原文 %q：%s", banned, msg)
		}
	}
}
