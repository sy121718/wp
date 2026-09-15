package feature

// page_translations_test.go — 翻译工作台（多语言 P5c，docs/06-D §7.8）真实链路验证。
//
// 链路：gin 路由 → dashboard handler（编排）→ builder 候选收集（与构建期同一函数）
// → pkg/i18n.ContentWriter（真 sys_translation 表）→ page 契约 MarkStaleForI18n（真 pages 表）。
//
// 覆盖：
//  1. 工作台渲染：分组行 / 原文 / 译文输入框 / 状态徽章 / engine 徽章 / 完成度 /
//     一键 AI 按钮灰置 + tooltip「待接入」/ 跨页面复用提示；
//  2. 保存：写入 sys_translation（engine=manual）+ 全站 stale 置位；
//  3. 幂等：同一条译文原样再保存 → 不写库、不触发全站重建；
//  4. 校验：白名单外字段 / 超长 / 富文本形态不符 / 指纹不符 / 未启用语言 → 拒绝且不落库；
//  5. 空译文 → 本行不写入（清空输入框不删除已有译文）。
//
// PG 不可用时 t.Skip（与其他功能测试一致）。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go_wp/internal/builder"
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// p5cPageDoc 工作台测试用页面文档：heading（纯文本字段）+ text（富文本）+ button。
const p5cPageDoc = "{\"settings\":{\"layout\":{\"mode\":\"full\"},\"seo\":{\"title\":\"关于我们\",\"description\":\"P5c\"}},\"root\":[" +
	"{\"id\":\"hd1\",\"type\":\"core.heading\",\"props\":{\"text\":\"关于我们\",\"subtitle\":\"了解更多\"}}," +
	"{\"id\":\"tx1\",\"type\":\"core.text\",\"props\":{\"text\":\"<p>我们成立于 2010 年</p>\"}}," +
	"{\"id\":\"bt1\",\"type\":\"core.button\",\"props\":{\"text\":\"联系我们\",\"link\":\"/contact\"}}]}"

// newTranslationEnv 装配工作台测试环境：真 PG（隔离 schema，全量迁移）+ project/page 契约
// + 隔离 schema 的内容译文写入器（SetContentTranslationStore）。
func newTranslationEnv(t *testing.T) (*gin.Engine, *dashboardhttp.Handle, *gorm.DB, string, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	gin.SetMode(gin.TestMode)
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil, nil, nil, "", ""
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(t.Context(), &projectdto.CreateReq{Name: "翻译工作台站点"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	if _, err := projects.SaveLocales(t.Context(), &projectdto.LocalesSaveReq{
		ProjectID: project.ID,
		Locales:   []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}, {Lang: "en-US"}},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}
	pageID := "aaaaaaaa-0000-0000-0000-000000000001"
	insertTranslationPage(t, db, pageID, project.ID, "/about", p5cPageDoc)

	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	handle := dashboardhttp.NewHandle(pages, projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handle.SetContentTranslationStore(i18n.NewContentWriter(db))

	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/page/translations", handle.PageTranslations)
	router.POST("/admin/page/translations/save", handle.SavePageTranslations)
	router.GET("/admin/pages", handle.PagesList)
	return router, handle, db, project.ID, pageID
}

// insertTranslationPage 直接插入一条页面行（含草稿文档），绕开路由/主题依赖。
func insertTranslationPage(t *testing.T, db *gorm.DB, id, projectID, path, doc string) {
	t.Helper()
	if err := db.Exec("INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, created_at, updated_at) "+
		"VALUES (?, ?, 'home', 'none', ?, ?::jsonb, 1, false, now(), now())",
		id, projectID, path, doc).Error; err != nil {
		t.Fatalf("插入页面失败: %v", err)
	}
}

// translationRowCount 统计某语言的译文行数。
func translationRowCount(t *testing.T, db *gorm.DB, lang string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_translation WHERE lang = ?", lang).Scan(&n).Error; err != nil {
		t.Fatalf("统计译文失败: %v", err)
	}
	return n
}

// translationTargetOf 读取一条译文的 (target_text, engine)。
func translationTargetOf(t *testing.T, db *gorm.DB, source, contextName, lang string) (target, engine string) {
	t.Helper()
	row := struct {
		TargetText string
		Engine     string
	}{}
	if err := db.Raw("SELECT target_text, engine FROM sys_translation WHERE source_hash = ? AND context = ? AND lang = ?",
		i18n.ContentHash(source), contextName, lang).Scan(&row).Error; err != nil {
		t.Fatalf("读取译文失败: %v", err)
	}
	return row.TargetText, row.Engine
}

// pageStale 读取页面 stale 标记。
func pageStale(t *testing.T, db *gorm.DB, id string) bool {
	t.Helper()
	var stale bool
	if err := db.Raw("SELECT stale FROM pages WHERE id = ?", id).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 stale 失败: %v", err)
	}
	return stale
}

// getTranslationPage GET 工作台并返回页面 HTML。
func getTranslationPage(t *testing.T, router *gin.Engine, pageID, lang string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/page/translations?pageId="+pageID+"&lang="+lang, nil)
	// 界面语言显式钉成中文（Cookie 优先级最高，协商顺序见 pkg/response.requestLanguage）。
	// 背景：本会话把后台模板全面 t() 化（admin.* 词条，迁移 192/195）之后，界面语言改按请求协商，
	// 而本 URL 上的 lang=en-US 是**翻译目标语言**，会被协商一并当成界面语言 —— 词条缓存已加载时
	// （整包跑，前面的测试初始化过 i18n）工作台整页渲染成英文，「本页完成度 0 / 3」这类中文断言即失败；
	// 单独跑该测试时缓存未加载、t 退回模板内中文，断言反而是绿的。
	// 钉住 Cookie 之后断言与词条缓存状态、测试执行顺序都无关；中文词条值（迁移 192）与模板 fallback
	// 逐字一致（含「本页完成度 」的尾空格），断言文本因此无需改动。
	req.AddCookie(&http.Cookie{Name: response.LangCookieName, Value: "zh-CN"})
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET 工作台 -> %d：%s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

// TestPageTranslationsWorkbenchRenders 工作台渲染：清单与构建期同源、状态/来源徽章、
// 完成度、一键 AI 灰置、跨页面复用提示。
func TestPageTranslationsWorkbenchRenders(t *testing.T) {
	router, _, db, projectID, pageID := newTranslationEnv(t)
	// 第二个页面复用同一句「了解更多」（跨页面复用提示）。
	insertTranslationPage(t, db, "bbbbbbbb-0000-0000-0000-000000000002", projectID, "/contact",
		"{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[{\"id\":\"hd2\",\"type\":\"core.heading\",\"props\":{\"text\":\"联系我们\",\"subtitle\":\"了解更多\"}}]}")

	body := getTranslationPage(t, router, pageID, "en-US")

	// 清单与构建期一致（机器证据）：工作台渲染的每一行 == builder.CollectContentCandidates
	// 对同一份草稿文档的输出（同函数、同白名单），行数也必须相等。
	parsed, err := builder.ParsePage([]byte(p5cPageDoc))
	if err != nil {
		t.Fatalf("解析页面文档失败: %v", err)
	}
	candidates := builder.CollectContentCandidates(parsed)
	if got := strings.Count(body, "name=\"rowContext\""); got != len(candidates) {
		t.Fatalf("工作台行数 %d 与构建期候选数 %d 不一致", got, len(candidates))
	}
	for _, cand := range candidates {
		if !strings.Contains(body, "name=\"rowContext\" value=\""+cand.Context+"\"") {
			t.Fatalf("工作台缺少构建期候选语境 %s", cand.Context)
		}
		if !strings.Contains(body, "name=\"rowHash\" value=\""+i18n.ContentHash(cand.Source)+"\"") {
			t.Fatalf("候选 %s（%q）的 source_hash 未出现在工作台", cand.Context, cand.Source)
		}
	}

	for _, want := range []string{
		"<h2>core.heading", "<h2>core.text", "<h2>core.button",
		"name=\"rowContext\" value=\"core.heading.text\"",
		"name=\"rowContext\" value=\"core.text.text\"",
		"name=\"rowContext\" value=\"core.button.text\"",
		"name=\"rowSource\" value=\"关于我们\"",
		"name=\"rowSource\" value=\"了解更多\"",
		"name=\"rowHash\" value=\"" + i18n.ContentHash("了解更多") + "\"",
		"缺失",
		"本页完成度 0 / 4",
		"全站完成度 en-US：0 / ",
		"disabled aria-disabled=\"true\" title=\"待接入\"",
		"一键 AI 翻译",
		"富文本译文需保留 HTML 标签",
		"上限 200 字节",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("工作台缺少 %q\n%s", want, body)
		}
	}
	for _, bad := range []string{"core.button.link", "core.text.advanced", "core.heading.level"} {
		if strings.Contains(body, bad) {
			t.Fatalf("工作台不应出现未声明字段 %q", bad)
		}
	}
	if !strings.Contains(body, "还用在另外 1 个页面（修改后全站同步生效）") {
		t.Fatalf("缺少跨页面复用提示\n%s", body)
	}
	if !strings.Contains(body, "该文本在 2 个页面出现") {
		t.Fatalf("缺少复用展开明细\n%s", body)
	}
	if n := strings.Count(body, "还用在另外"); n != 1 {
		t.Fatalf("复用提示应只出现在共用的那一行，实际出现 %d 次", n)
	}
}

// TestPagesListShowsTranslationsEntry 页面列表行内「多语言」入口指向本页工作台（决策 F10：不做独立菜单）。
func TestPagesListShowsTranslationsEntry(t *testing.T) {
	router, _, _, _, pageID := newTranslationEnv(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /admin/pages -> %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "/admin/page/translations?pageId="+pageID) {
		t.Fatalf("页面列表缺少「多语言」入口链接\n%s", body)
	}
	if !strings.Contains(body, ">多语言</a>") {
		t.Fatalf("页面列表缺少「多语言」按钮文案\n%s", body)
	}
}

// TestSavePageTranslationsWritesAndMarksStale 保存 → 落库（engine=manual）+ 全站 stale 置位。
func TestSavePageTranslationsWritesAndMarksStale(t *testing.T) {
	router, _, db, projectID, pageID := newTranslationEnv(t)
	insertTranslationPage(t, db, "cccccccc-0000-0000-0000-000000000003", projectID, "/other",
		"{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[{\"id\":\"bt2\",\"type\":\"core.button\",\"props\":{\"text\":\"联系我们\"}}]}")

	saved := postForm(t, router, "/admin/page/translations/save", url.Values{
		"pageId": {pageID}, "lang": {"en-US"},
		"rowContext": {"core.button.text", "core.text.text"},
		"rowSource":  {"联系我们", "<p>我们成立于 2010 年</p>"},
		"rowHash":    {i18n.ContentHash("联系我们"), i18n.ContentHash("<p>我们成立于 2010 年</p>")},
		"rowTarget":  {"Contact us", "<p>Founded in 2010</p>"},
	})
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("保存成功应 303 回跳，实际 %d：%s", saved.Code, saved.Body.String())
	}
	if loc := saved.Header().Get("Location"); !strings.Contains(loc, "saved=1") || !strings.Contains(loc, "n=2") {
		t.Fatalf("回跳 URL 应带写入条数 n=2，实际 %q", loc)
	}
	if n := translationRowCount(t, db, "en-US"); n != 2 {
		t.Fatalf("应写入 2 条译文，实际 %d", n)
	}
	target, engine := translationTargetOf(t, db, "联系我们", "core.button.text", "en-US")
	if target != "Contact us" || engine != i18n.ContentEngineManual {
		t.Fatalf("译文/来源不符: %q / %q", target, engine)
	}
	for _, id := range []string{pageID, "cccccccc-0000-0000-0000-000000000003"} {
		if !pageStale(t, db, id) {
			t.Fatalf("页面 %s 应被标记 stale=true", id)
		}
	}
	body := getTranslationPage(t, router, pageID, "en-US")
	for _, want := range []string{"已翻译", ">manual<", "本页完成度 2 / 4"} {
		if !strings.Contains(body, want) {
			t.Fatalf("保存后工作台缺少 %q\n%s", want, body)
		}
	}
}

// TestSavePageTranslationsIdempotent 原样再保存 → 不写库、不触发全站重建。
func TestSavePageTranslationsIdempotent(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	form := url.Values{
		"pageId": {pageID}, "lang": {"en-US"},
		"rowContext": {"core.button.text"},
		"rowSource":  {"联系我们"},
		"rowHash":    {i18n.ContentHash("联系我们")},
		"rowTarget":  {"Contact us"},
	}
	if saved := postForm(t, router, "/admin/page/translations/save", form); saved.Code != http.StatusSeeOther {
		t.Fatalf("首次保存应 303，实际 %d：%s", saved.Code, saved.Body.String())
	}
	var firstUpdated time.Time
	if err := db.Raw("SELECT updated_at FROM sys_translation").Scan(&firstUpdated).Error; err != nil {
		t.Fatalf("读取 updated_at 失败: %v", err)
	}
	if err := db.Exec("UPDATE pages SET stale = false WHERE id = ?", pageID).Error; err != nil {
		t.Fatalf("复位 stale 失败: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	again := postForm(t, router, "/admin/page/translations/save", form)
	if again.Code != http.StatusSeeOther {
		t.Fatalf("重复保存应 303，实际 %d：%s", again.Code, again.Body.String())
	}
	if loc := again.Header().Get("Location"); !strings.Contains(loc, "n=0") {
		t.Fatalf("幂等保存应写入 0 条，实际回跳 %q", loc)
	}
	var secondUpdated time.Time
	if err := db.Raw("SELECT updated_at FROM sys_translation").Scan(&secondUpdated).Error; err != nil {
		t.Fatalf("读取 updated_at 失败: %v", err)
	}
	if !secondUpdated.Equal(firstUpdated) {
		t.Fatalf("幂等保存不应改写译文行：before=%s after=%s", firstUpdated, secondUpdated)
	}
	if pageStale(t, db, pageID) {
		t.Fatal("译文未变化不应触发全站标记待重建")
	}
}

// TestSavePageTranslationsRejectsInvalid 校验失败 → 不落库、不触发重建、页面回显错误。
func TestSavePageTranslationsRejectsInvalid(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)

	cases := []struct {
		name    string
		context string
		source  string
		hash    string
		target  string
		want    string
	}{
		{"白名单外字段", "core.button.link", "/contact", i18n.ContentHash("/contact"), "/contact-en", "语境非法"},
		{"超长译文", "core.button.text", "联系我们", i18n.ContentHash("联系我们"), strings.Repeat("a", 201), "超过该字段的长度上限"},
		{"富文本形态不符", "core.text.text", "<p>我们成立于 2010 年</p>", i18n.ContentHash("<p>我们成立于 2010 年</p>"), "Founded in 2010", "译文形态与原文不一致"},
		{"原文指纹不符", "core.button.text", "联系我们", i18n.ContentHash("别的原文"), "Contact us", "原文已变更"},
		{"跳过规则原文", "core.text.text", "2024", i18n.ContentHash("2024"), "2024", "不参与翻译"},
	}
	for _, tc := range cases {
		recorder := postForm(t, router, "/admin/page/translations/save", url.Values{
			"pageId": {pageID}, "lang": {"en-US"},
			"rowContext": {tc.context}, "rowSource": {tc.source},
			"rowHash": {tc.hash}, "rowTarget": {tc.target},
		})
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s：校验失败应回渲染工作台（200），实际 %d", tc.name, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), tc.want) {
			t.Fatalf("%s：缺少错误提示 %q\n%s", tc.name, tc.want, recorder.Body.String())
		}
		if n := translationRowCount(t, db, "en-US"); n != 0 {
			t.Fatalf("%s：校验失败不应落库，实际 %d 行", tc.name, n)
		}
		if pageStale(t, db, pageID) {
			t.Fatalf("%s：校验失败不应触发全站重建", tc.name)
		}
	}
}

// TestSavePageTranslationsEmptyTargetSkips 空译文 → 本行不写入（也不触发重建）。
func TestSavePageTranslationsEmptyTargetSkips(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	recorder := postForm(t, router, "/admin/page/translations/save", url.Values{
		"pageId": {pageID}, "lang": {"en-US"},
		"rowContext": {"core.button.text"}, "rowSource": {"联系我们"},
		"rowHash": {i18n.ContentHash("联系我们")}, "rowTarget": {"   "},
	})
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("空译文应正常回跳（不写库），实际 %d", recorder.Code)
	}
	if loc := recorder.Header().Get("Location"); !strings.Contains(loc, "n=0") {
		t.Fatalf("空译文不应写入，实际回跳 %q", loc)
	}
	if n := translationRowCount(t, db, "en-US"); n != 0 {
		t.Fatalf("空译文不应落库，实际 %d 行", n)
	}
	if pageStale(t, db, pageID) {
		t.Fatal("空译文不应触发全站重建")
	}
}

// TestSavePageTranslationsLangNotEnabled 未启用的语言 → 拒绝写入。
func TestSavePageTranslationsLangNotEnabled(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	recorder := postForm(t, router, "/admin/page/translations/save", url.Values{
		"pageId": {pageID}, "lang": {"ja"},
		"rowContext": {"core.button.text"}, "rowSource": {"联系我们"},
		"rowHash": {i18n.ContentHash("联系我们")}, "rowTarget": {"お問い合わせ"},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("未启用语言应回渲染工作台（200），实际 %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "目标语言未启用") {
		t.Fatalf("缺少未启用语言提示\n%s", recorder.Body.String())
	}
	if n := translationRowCount(t, db, "ja"); n != 0 {
		t.Fatalf("未启用语言不应落库，实际 %d 行", n)
	}
}
