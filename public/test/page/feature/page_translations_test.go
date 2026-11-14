package feature

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	pageenums "go_wp/internal/module/page/enums"
	pagehttp "go_wp/internal/module/page/inbound/http"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/builder"
	"go_wp/pkg/i18n"
	jsonx "go_wp/pkg/jsonx"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const p5cPageDoc = "{\"settings\":{\"layout\":{\"mode\":\"full\"},\"seo\":{\"title\":\"关于我们\",\"description\":\"P5c\"}},\"root\":[" +
	"{\"id\":\"hd1\",\"type\":\"core.heading\",\"props\":{\"text\":\"关于我们\",\"subtitle\":\"了解更多\"}}," +
	"{\"id\":\"tx1\",\"type\":\"core.text\",\"props\":{\"text\":\"<p>我们成立于 2010 年</p>\"}}," +
	"{\"id\":\"bt1\",\"type\":\"core.button\",\"props\":{\"text\":\"联系我们\",\"link\":\"/contact\"}}]}"

func newTranslationEnv(t *testing.T) (*gin.Engine, *pagehttp.PagesAdminHandle, *gorm.DB, string, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	gin.SetMode(gin.TestMode)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, nil, "", ""
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
	handle := pagehttp.NewPagesAdminHandle(pages, projects, nil, nil)
	handle.SetContentTranslationStore(i18n.NewContentWriter(db))

	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	router.Use(pageview.UseRenderer(pageview.RenderFragment))
	router.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.GET("/admin/pages/translations", pageview.Slot(pagehttp.PageTranslationsTemplate, pageview.RenderPage), handle.PageTranslations)
	router.POST("/api/page/translations/save", pageview.HTMLFragment, handle.SavePageTranslations)
	router.GET("/admin/pages", pageview.Slot(pagehttp.PagesListPageTemplate, pageview.RenderPage), handle.PagesList)
	return router, handle, db, project.ID, pageID
}

func insertTranslationPage(t *testing.T, db *gorm.DB, id, projectID, path, doc string) {
	t.Helper()
	if err := db.Exec("INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, create_time, update_time) "+
		"VALUES (?, ?, 'home', 'none', ?, ?::jsonb, 1, false, now(), now())",
		id, projectID, path, doc).Error; err != nil {
		t.Fatalf("插入页面失败: %v", err)
	}
}

// decodeWritePayload 解写出口的 {code, message, data}。只在这一族断言里用，
// 所以放在本文件而不是抽公共 helper。
func decodeWritePayload(t *testing.T, body string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := jsonx.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("写出口应为 JSON：%v\n%s", err, body)
	}
	return payload
}

// decodeWriteItems 取 data.items（写基座的错误明细档，形状对齐 blockedDataOf）。
func decodeWriteItems(t *testing.T, body string) []map[string]any {
	t.Helper()
	data, _ := decodeWritePayload(t, body)["data"].(map[string]any)
	raw, _ := data["items"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, v := range raw {
		if row, ok := v.(map[string]any); ok {
			items = append(items, row)
		}
	}
	return items
}

func translationRowCount(t *testing.T, db *gorm.DB, lang string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_translation WHERE lang = ?", lang).Scan(&n).Error; err != nil {
		t.Fatalf("统计译文失败: %v", err)
	}
	return n
}

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

func pageStale(t *testing.T, db *gorm.DB, id string) bool {
	t.Helper()
	var stale bool
	if err := db.Raw("SELECT stale FROM pages WHERE id = ?", id).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 stale 失败: %v", err)
	}
	return stale
}

func getTranslationPage(t *testing.T, router *gin.Engine, pageID, lang string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/pages/translations?pageId="+pageID+"&lang="+lang, nil)

	req.AddCookie(&http.Cookie{Name: response.LangCookieName, Value: "zh-CN"})
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET 工作台 -> %d：%s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func TestPageTranslationsWorkbenchRenders(t *testing.T) {
	router, _, db, projectID, pageID := newTranslationEnv(t)

	insertTranslationPage(t, db, "bbbbbbbb-0000-0000-0000-000000000002", projectID, "/contact",
		"{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[{\"id\":\"hd2\",\"type\":\"core.heading\",\"props\":{\"text\":\"联系我们\",\"subtitle\":\"了解更多\"}}]}")

	body := getTranslationPage(t, router, pageID, "en-US")

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

		`<td colspan="6">core.heading `, `<td colspan="6">core.text `, `<td colspan="6">core.button `,
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

	if n := strings.Count(body, `class="data-table data-table-wide tr-table"`); n != 1 {
		t.Fatalf("多组件应共用一张编辑表，实际 %d 张", n)
	}

	if tables, heads := strings.Count(body, "class=\"data-table"), strings.Count(body, "<thead>"); tables != heads {
		t.Fatalf("每张数据表应当恰好一个表头，实际 %d 张表 / %d 个表头", tables, heads)
	}

	for _, want := range []struct {
		comp string
		rows int
	}{{"core.heading", 2}, {"core.text", 1}, {"core.button", 1}} {
		pat := `<td colspan="6">` + want.comp + ` <span[^>]*>（` + strconv.Itoa(want.rows) + ` 条）</span>`
		if !regexp.MustCompile(pat).MatchString(body) {
			t.Fatalf("分组行缺少组件 %s 的本组条数（期望 %d 条）", want.comp, want.rows)
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

func TestPagesListShowsTranslationsEntry(t *testing.T) {
	router, _, _, _, pageID := newTranslationEnv(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /admin/pages -> %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "/admin/pages/translations?pageId="+pageID) {
		t.Fatalf("页面列表缺少「多语言」入口链接\n%s", body)
	}
	if !strings.Contains(body, ">多语言</a>") {
		t.Fatalf("页面列表缺少「多语言」按钮文案\n%s", body)
	}

	if strings.Contains(body, "/admin/page-langs/panel") {
		t.Fatalf("页面列表仍有已删除的语言面板入口\n%s", body)
	}
}

func TestPageTranslationsRendersLangScope(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	body := getTranslationPage(t, router, pageID, "en-US")
	for _, want := range []string{
		"语言产出范围",
		`action="/admin/page-langs/exclude"`,
		`action="/admin/page-langs/republish"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("工作台缺少产出范围元素 %q\n%s", want, body)
		}
	}
	if !strings.Contains(body, "始终产出") {
		t.Fatalf("默认语言那一行应说明不可排除\n%s", body)
	}

	if err := db.Exec("UPDATE pages SET excluded_langs = ARRAY['en-US']::text[] WHERE id = ?", pageID).Error; err != nil {
		t.Fatalf("设置排除列失败: %v", err)
	}
	body = getTranslationPage(t, router, pageID, "en-US")
	if !strings.Contains(body, `action="/admin/page-langs/restore"`) {
		t.Fatalf("已排除语言应给「恢复」入口\n%s", body)
	}
	if !strings.Contains(body, "已排除") {
		t.Fatalf("已排除语言的状态徽章缺失\n%s", body)
	}
}

func TestSavePageTranslationsWritesAndMarksStale(t *testing.T) {
	router, _, db, projectID, pageID := newTranslationEnv(t)
	insertTranslationPage(t, db, "cccccccc-0000-0000-0000-000000000003", projectID, "/other",
		"{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[{\"id\":\"bt2\",\"type\":\"core.button\",\"props\":{\"text\":\"联系我们\"}}]}")

	saved := postForm(t, router, "/api/page/translations/save", url.Values{
		"pageId": {pageID}, "lang": {"en-US"},
		"rowContext": {"core.button.text", "core.text.text"},
		"rowSource":  {"联系我们", "<p>我们成立于 2010 年</p>"},
		"rowHash":    {i18n.ContentHash("联系我们"), i18n.ContentHash("<p>我们成立于 2010 年</p>")},
		"rowTarget":  {"Contact us", "<p>Founded in 2010</p>"},
	})

	if saved.Code != http.StatusOK || !strings.Contains(saved.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("保存成功应回 HTML 200，实际 %d：%s", saved.Code, saved.Body.String())
	}
	body := saved.Body.String()
	if !strings.Contains(body, "已保存 2 条译文") {
		t.Fatalf("成功响应应含完整回执句：%s", body)
	}
	if strings.Contains(body, `data-jump-state="ok"`) {
		t.Fatalf("成功响应不该再渲染提示页：%s", body)
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
	pageBody := getTranslationPage(t, router, pageID, "en-US")
	for _, want := range []string{"已翻译", ">manual<", "本页完成度 2 / 4"} {
		if !strings.Contains(pageBody, want) {
			t.Fatalf("保存后工作台缺少 %q\n%s", want, pageBody)
		}
	}
}

type renderedTranslationRow struct {
	Context string
	Source  string
	Hash    string
}

var (
	renderedContextRe = regexp.MustCompile(`name="rowContext" value="([^"]*)"`)
	renderedSourceRe  = regexp.MustCompile(`name="rowSource" value="([^"]*)"`)
	renderedHashRe    = regexp.MustCompile(`name="rowHash" value="([^"]*)"`)
)

func parseRenderedTranslationRows(t *testing.T, body string) []renderedTranslationRow {
	t.Helper()
	var out []renderedTranslationRow
	for i, chunk := range strings.Split(body, "<tr>")[1:] {
		if !strings.Contains(chunk, `name="rowTarget"`) {
			continue
		}
		row := chunk
		if j := strings.Index(row, "</tr>"); j >= 0 {
			row = row[:j]
		}
		pick := func(re *regexp.Regexp, what string) string {
			m := re.FindStringSubmatch(row)
			if m == nil {
				t.Fatalf("第 %d 个数据行缺少 %s（四个字段必须同处一个 tr）", i, what)
			}
			return html.UnescapeString(m[1])
		}
		out = append(out, renderedTranslationRow{
			Context: pick(renderedContextRe, "rowContext"),
			Source:  pick(renderedSourceRe, "rowSource"),
			Hash:    pick(renderedHashRe, "rowHash"),
		})
	}
	return out
}

func TestSavePageTranslationsFromRenderedForm(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	body := getTranslationPage(t, router, pageID, "en-US")

	rows := parseRenderedTranslationRows(t, body)
	if len(rows) == 0 {
		t.Fatal("渲染出来的表单里没有可提交的行")
	}

	for _, name := range []string{"rowContext", "rowSource", "rowHash"} {
		if got := strings.Count(body, `name="`+name+`"`); got != len(rows) {
			t.Fatalf("rowTarget 解析出 %d 行，而 %s 有 %d 个 —— 四个数组不等长会被整体拒绝",
				len(rows), name, got)
		}
	}

	form := url.Values{"pageId": {pageID}, "lang": {"en-US"}}
	nonEmpty := 0
	for _, r := range rows {
		target := ""

		if r.Context == "core.heading.text" || r.Context == "core.button.text" {
			target = "Target-" + r.Context
			nonEmpty++
		}
		form.Add("rowTarget", target)
		form.Add("rowContext", r.Context)
		form.Add("rowSource", r.Source)
		form.Add("rowHash", r.Hash)
	}
	if nonEmpty == 0 {
		t.Fatal("没有非空输入，用例失去意义")
	}

	saved := postForm(t, router, "/api/page/translations/save", form)
	if saved.Code != http.StatusOK {
		t.Fatalf("保存应渲染提示页（200），实际 %d：%s", saved.Code, saved.Body.String())
	}
	if body := saved.Body.String(); !strings.Contains(body, "已保存 "+strconv.Itoa(nonEmpty)+" 条译文") {
		t.Fatalf("非空输入 %d 条与回执计数不符：%s", nonEmpty, body)
	}
	if n := translationRowCount(t, db, "en-US"); n != int64(nonEmpty) {
		t.Fatalf("落库 %d 条 != 非空输入数 %d", n, nonEmpty)
	}
}

func TestSavePageTranslationsIdempotent(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	form := url.Values{
		"pageId": {pageID}, "lang": {"en-US"},
		"rowContext": {"core.button.text"},
		"rowSource":  {"联系我们"},
		"rowHash":    {i18n.ContentHash("联系我们")},
		"rowTarget":  {"Contact us"},
	}
	if saved := postForm(t, router, "/api/page/translations/save", form); saved.Code != http.StatusOK {
		t.Fatalf("首次保存应渲染提示页（200），实际 %d：%s", saved.Code, saved.Body.String())
	}
	var firstUpdated time.Time
	if err := db.Raw("SELECT update_time FROM sys_translation").Scan(&firstUpdated).Error; err != nil {
		t.Fatalf("读取 update_time 失败: %v", err)
	}
	if err := db.Exec("UPDATE pages SET stale = false WHERE id = ?", pageID).Error; err != nil {
		t.Fatalf("复位 stale 失败: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	again := postForm(t, router, "/api/page/translations/save", form)
	if again.Code != http.StatusOK {
		t.Fatalf("重复保存应渲染提示页（200），实际 %d：%s", again.Code, again.Body.String())
	}
	if body := again.Body.String(); !strings.Contains(body, "已保存 0 条译文") {
		t.Fatalf("幂等保存应写入 0 条，实际提示 %s", body)
	}
	var secondUpdated time.Time
	if err := db.Raw("SELECT update_time FROM sys_translation").Scan(&secondUpdated).Error; err != nil {
		t.Fatalf("读取 update_time 失败: %v", err)
	}
	if !secondUpdated.Equal(firstUpdated) {
		t.Fatalf("幂等保存不应改写译文行：before=%s after=%s", firstUpdated, secondUpdated)
	}
	if pageStale(t, db, pageID) {
		t.Fatal("译文未变化不应触发全站标记待重建")
	}
}

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
		recorder := postForm(t, router, "/api/page/translations/save", url.Values{
			"pageId": {pageID}, "lang": {"en-US"},
			"rowContext": {tc.context}, "rowSource": {tc.source},
			"rowHash": {tc.hash}, "rowTarget": {tc.target},
		})

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s：校验失败应回 400，实际 %d：%s", tc.name, recorder.Code, recorder.Body.String())
		}
		// 逐行原因由 Jet 片段渲染：错误区里必须有行标识与该原因。
		body := recorder.Body.String()
		if !strings.Contains(body, "data-ui-error") || !strings.Contains(body, tc.context) || !strings.Contains(body, tc.want) {
			t.Fatalf("%s：错误区应带行标识与原因，实际 %s", tc.name, body)
		}
		if n := translationRowCount(t, db, "en-US"); n != 0 {
			t.Fatalf("%s：校验失败不应落库，实际 %d 行", tc.name, n)
		}
		if pageStale(t, db, pageID) {
			t.Fatalf("%s：校验失败不应触发全站重建", tc.name)
		}
	}
}

func TestSavePageTranslationsEmptyTargetSkips(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	recorder := postForm(t, router, "/api/page/translations/save", url.Values{
		"pageId": {pageID}, "lang": {"en-US"},
		"rowContext": {"core.button.text"}, "rowSource": {"联系我们"},
		"rowHash": {i18n.ContentHash("联系我们")}, "rowTarget": {"   "},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("空译文应正常渲染提示页（不写库），实际 %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "已保存 0 条译文") {
		t.Fatalf("空译文不应写入，实际提示 %s", body)
	}
	if n := translationRowCount(t, db, "en-US"); n != 0 {
		t.Fatalf("空译文不应落库，实际 %d 行", n)
	}
	if pageStale(t, db, pageID) {
		t.Fatal("空译文不应触发全站重建")
	}
}

func TestSavePageTranslationsLangNotEnabled(t *testing.T) {
	router, _, db, _, pageID := newTranslationEnv(t)
	recorder := postForm(t, router, "/api/page/translations/save", url.Values{
		"pageId": {pageID}, "lang": {"ja"},
		"rowContext": {"core.button.text"}, "rowSource": {"联系我们"},
		"rowHash": {i18n.ContentHash("联系我们")}, "rowTarget": {"お問い合わせ"},
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("未启用语言应回 400，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("未启用语言应渲染错误区，实际 %s", body)
	}
	// 逐行原因由 Jet 取词，页面上不能出现 enums 裸 key。
	if strings.Contains(body, pageenums.MsgTranslationLangInvalid) {
		t.Fatalf("逐行原因应已是译文而非裸 key，实际 %s", body)
	}
	if n := translationRowCount(t, db, "ja"); n != 0 {
		t.Fatalf("未启用语言不应落库，实际 %d 行", n)
	}
}
