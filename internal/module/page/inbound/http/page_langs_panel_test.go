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
	"go_wp/internal/templates"
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
func TestPageLangsPanelRenders(t *testing.T) {
	svc, projects, pageID, _ := seedLangPanelFixture(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	h := pagehttp.NewPagesAdminHandle(svc, projects, nil, nil)
	router.GET("/admin/page-langs/panel", h.PageLangsPanel)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/page-langs/panel?pageId="+pageID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("面板应渲染成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"page-langs-" + pageID, // 片段根（HTMX swap 目标）
		`name="csrf_token"`,    // 原生表单必须显式带 CSRF 隐藏域
		"en-AU", "zh-CN",       // 两种启用语言都在
		`action="/admin/page-langs/exclude"`, // 未排除的非默认语言给「排除」
		"站点默认语言",                             // 默认语言不可排除的原因（中文兜底）
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("面板缺少元素 %q：body=%s", want, body)
		}
	}
	if strings.Contains(body, `action="/admin/page-langs/restore"`) {
		t.Fatal("没有排除任何语言时不该出现「恢复」表单（会让运营以为已经排除了）")
	}

	// 另一个页面已经把 zh-CN 标成排除：操作列应切换成「恢复」。
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet,
		"/admin/page-langs/panel?pageId=30000000-0000-0000-0000-0000000000a2", nil))
	body2 := rec2.Body.String()
	if !strings.Contains(body2, `action="/admin/page-langs/restore"`) {
		t.Fatalf("已排除语言应给「恢复」表单：body=%s", body2)
	}
	// V4：已排除的语言**不给**「重新发布」——先恢复再重发，避免「点了重发却什么都没发生」
	// （发布入口对被排除语言会直接拒绝，那是设计上的显式失败）。
	rowsSegment := body2[strings.Index(body2, "已排除（不产出）"):]
	if strings.Contains(rowsSegment, `action="/admin/page-langs/republish"`) {
		t.Fatalf("已排除的语言不该给「重新发布」按钮：body=%s", body2)
	}
	if !strings.Contains(body2, "已排除（不产出）") {
		t.Fatalf("已排除语言应显示「已排除（不产出）」状态：body=%s", body2)
	}

	// 片段必须渲染完整（最后一层 div 闭合）：模板中途出错时 Jet 会写半截内容。
	if !strings.HasSuffix(strings.TrimSpace(body), "</div>") {
		t.Fatalf("片段没有渲染完整：%s", body)
	}
}

// TestPageLangsPanelDegradesOnLoadFailure 页面读不到（id 不存在）时仍渲染完整片段：
// 表头常驻 + 空态整行进 tbody（colspan = 列数）+ 归口文案（不直出内部错误）。
//
// 为什么这条重要：面板是 HTMX 片段，渲染失败时页面上**什么都不显示**（inert 的失败）——
// 运营点了「语言」按钮却没有任何反应，而日志里可能什么都没有。
func TestPageLangsPanelDegradesOnLoadFailure(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	projectID := "30000000-0000-0000-0000-000000000002"
	support.SeedProjectRow(t, db, projectID, "降级用例")
	projects := langPanelProjectList{ids: []string{projectID}, langs: []string{"en-AU", "zh-CN"}}
	svc := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	h := pagehttp.NewPagesAdminHandle(svc, projects, nil, nil)
	router.GET("/admin/page-langs/panel", h.PageLangsPanel)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/page-langs/panel?pageId=30000000-0000-0000-0000-0000000000ff", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("读不到页面时应降级渲染（HTMX 对 4xx/5xx 不替换节点），实际 %d", rec.Code)
	}
	if !strings.Contains(body, "<thead>") {
		t.Fatalf("空态下 table 表头必须常驻：%s", body)
	}
	if !strings.Contains(body, `colspan="4"`) {
		t.Fatalf("空态行应整行进 tbody 且 colspan 等于列数（4）：%s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</div>") {
		t.Fatalf("片段没有渲染完整：%s", body)
	}
	// 归口文案（不直出内部错误）：页面不存在是业务错误，应给可读文案而不是 SQL 原文。
	if strings.Contains(body, "SQLSTATE") || strings.Contains(body, "record not found") {
		t.Fatalf("片段里出现了内部错误原文：%s", body)
	}
}
