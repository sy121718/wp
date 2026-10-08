package contenthttp

// article_page_render_test.go — 文章管理页与评测侧栏的渲染冒烟（INF-1 / SEO-10）。
//
// 钉住三件事：
//   - admin/articles.html、admin/article_edit.html 与 fragments/seo_score.html 的
//     Jet 语法与 layout 数据契约成立（模板错了只会在运营点开时 500）；
//   - 服务端给的字段真的渲染出来了（不是「handler 对了、页面空白」）；
//   - 发布区块的四种形态各自渲染成什么（已发布 / 可发布 / 无模板 / 能力缺失）——
//     这是本页最容易「看起来正常其实不可用」的地方。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	"go_wp/internal/templates"
)

// articleLayoutData 补齐 layout 需要的键（与线上 shell.Prepare / withI18n 注入的一致）。
func articleLayoutData(base gin.H) gin.H {
	lang := "zh-CN"
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/articles"
	// 新建入口进抽屉后按权限显隐，抽屉 <template> 只在有权限时渲染 —— 测试数据给全集
	//（运行时由 shell.Prepare 注入该用户拥有的权限码）。
	base["Buttons"] = map[string]any{"content.create": true, "content.update": true, "content.delete": true}
	return base
}

// renderAdminTemplate 用真实 Jet 渲染器渲染一个后台模板并返回 HTML。
func renderAdminTemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根目录相对包目录：internal/module/content/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/page", func(c *gin.Context) { c.HTML(http.StatusOK, name, data) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s 渲染失败，状态 %d", name, rec.Code)
	}
	return rec.Body.String()
}

func TestArticlesListTemplateRenders(t *testing.T) {
	list := []*contentdto.ContentResp{
		{ID: "a1", Slug: "hello-world", Revision: 3, UpdatedAt: "2026-09-13 10:00",
			Data: map[string]any{"title": "第一篇", "excerpt": "摘要一"}},
		{ID: "a2", Slug: "second-post", Revision: 1, UpdatedAt: "2026-09-12 09:00",
			Data: map[string]any{"title": "第二篇"}},
	}
	data := articleListPageData(list, map[string]string{"a1": "/blog/hello-world"}, "")
	body := renderAdminTemplate(t, "admin/content/articles.html", articleLayoutData(data))

	for _, want := range []string{
		"第一篇", "hello-world", "第二篇", "second-post",
		"已发布", "未发布", "/site/blog/hello-world", "/admin/articles/edit?id=a1",
		// 新建入口（弃抽屉）改为整页链接，抽屉模板不再渲染。
		"/admin/articles/new", "删除",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页渲染结果缺少 %q", want)
		}
	}
}

type articleSearchContentStub struct {
	contentcontract.ContentService
	countReq contentdto.ListReq
	listReq  contentdto.ListReq
	total    int64
}

func (s *articleSearchContentStub) Count(_ context.Context, req *contentdto.ListReq) (int64, error) {
	s.countReq = *req
	return s.total, nil
}

func (s *articleSearchContentStub) List(_ context.Context, req *contentdto.ListReq) ([]*contentdto.ContentResp, error) {
	s.listReq = *req
	if s.total == 0 {
		return nil, nil
	}
	return []*contentdto.ContentResp{{ID: "a2", Slug: "needle-two", Data: map[string]any{"title": "第二篇匹配"}}}, nil
}

func TestArticlesPageKeywordFiltersBeforePagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	contents := &articleSearchContentStub{total: 2}
	h := &articlePageHandle{contents: contents}
	engine.GET("/admin/articles", h.ArticlesPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/articles?keyword=+needle+&page=99&limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("搜索列表响应状态 %d: %s", rec.Code, rec.Body.String())
	}
	if contents.countReq.EntityType != "article" || contents.countReq.Keyword != "needle" {
		t.Errorf("计数筛选条件错误: %+v", contents.countReq)
	}
	if contents.listReq.EntityType != "article" || contents.listReq.Keyword != "needle" ||
		contents.listReq.Limit != 1 || contents.listReq.Offset != 1 {
		t.Errorf("列表应按筛选后的总数收敛页码并取第二页: %+v", contents.listReq)
	}
	body := rec.Body.String()
	for _, want := range []string{`name="keyword" value="needle"`, `name="limit" value="1"`, `href="/admin/articles?keyword=needle&amp;page=1&amp;limit=1"`, `href="/admin/articles?limit=1"`, "第二篇匹配",
		// 写动作的表单 action 带上本次筛选（BackPath 从这次请求的 query 读回）：删完 / 批量删完
		// 回列表页时关键词与每页条数不丢。
		`action="/admin/articles/bulk-delete?keyword=needle&amp;limit=1"`,
		`action="/admin/articles/delete?keyword=needle&amp;limit=1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("搜索列表缺少 %q", want)
		}
	}
	if strings.Contains(body, `data-filter-input`) {
		t.Error("旧的当前页 DOM 过滤入口仍然存在")
	}
}

func TestArticlesPageKeywordNoMatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	contents := &articleSearchContentStub{total: 0}
	engine.GET("/admin/articles", (&articlePageHandle{contents: contents}).ArticlesPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/articles?keyword=missing&page=8", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("无匹配搜索响应状态 %d: %s", rec.Code, rec.Body.String())
	}
	if contents.countReq.Keyword != "missing" || contents.listReq.Keyword != "missing" || contents.listReq.Offset != 0 {
		t.Errorf("无匹配应从第一页取筛选结果: count=%+v list=%+v", contents.countReq, contents.listReq)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "没有匹配的文章") || !strings.Contains(body, `href="/admin/articles?limit=20"`) {
		t.Error("无匹配空态应提供清除筛选入口")
	}
	if strings.Contains(body, "还没有文章") || strings.Contains(body, `class="pagination"`) {
		t.Error("无匹配不应显示首次创建空态或分页条")
	}
}

func TestArticleEditTemplateRendersSEOFields(t *testing.T) {
	item := &contentdto.ContentResp{ID: "a1", Slug: "hello-world", Revision: 4, UpdatedAt: "2026-09-13 10:00",
		Data: map[string]any{
			"title": "第一篇", "body": "<p>正文</p>", "excerpt": "摘要一",
			"seoTitle": "SEO 标题", "seoDescription": "SEO 描述", "focusKeyword": "关键词",
			"featuredImage": "/storage/image/cover.webp",
		}}
	data := articleEditPageData(context.Background(), &articlePageHandle{}, item, "a1", "", "zh-CN")
	body := renderAdminTemplate(t, "admin/content/article_edit.html", articleLayoutData(data))

	for _, want := range []string{
		"第一篇", "hello-world", "摘要一", "关键词",
		"/storage/image/cover.webp",
		// 正文编辑器与商品分类 / 品牌描述共用 partials/rich_editor.html：隐藏 input（name=body）、
		// trix-editor、扩展入口与扩展样式都由片段给出 —— 这里钉住它们真的渲染出来了
		//（片段没接上时页面不报错，只是编辑器退化成一块没有工具条的空白区）。
		"trix-editor", `rich-body-article`, `data-rich-editor="body"`, `name="body"`,
		"/static/js/rich-editor/index.js", "/static/css/rich-editor.css",
		"/admin/articles/update",
		// SEO 评测已从右栏常驻卡改成抽屉：编辑页本身只留**入口**，评测内容与「重新评分」
		// 都在抽屉片段里（片段自身的断言见下一个用例）。
		"SEO 评测", `data-drawer-url="/admin/articles/seo/drawer?id=`,
		"保存",
		// 导入到画布区块（06-B 决策 5 的入口）：能力未装配时给提示而不是按钮。
		// 可视化编辑（原「导入到画布」的直白措辞，行为不变：复制而非绑定）+ 详情页真实预览帧。
		"可视化编辑", "article-preview-frame",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑页渲染结果缺少 %q", want)
		}
	}
	// SEO 标题 / 描述已与标题 / 摘要合并（2026-09-30）：数据里遗留的同名字段
	// 不该再被渲染成输入框（旧值仍可读，但编辑入口只有一个）。
	for _, gone := range []string{`name="seoTitle"`, `name="seoDescription"`, "SEO 标题", "SEO 描述"} {
		if strings.Contains(body, gone) {
			t.Errorf("SEO 字段已合并，编辑页不该再出现 %q", gone)
		}
	}
	// 页级动作归位（清单 02-L P1-8）：保存按钮必须在 .page-head 的 .page-actions 里，
	// 且全页只有这一处 —— 它是这一版唯一的保存入口（原 sticky 贴底保存卡已移除）。
	head := articleSection(t, body, `class="page-actions"`, "</header>")
	if n := strings.Count(head, `form="article-form"`); n != 1 {
		t.Errorf(".page-actions 里 form=\"article-form\" 的按钮应为 1 个，实际 %d", n)
	}
	if n := strings.Count(body, `form="article-form"`); n != 1 {
		t.Errorf("全页 form=\"article-form\" 的按钮应为 1 个，实际 %d", n)
	}
	// 「保存卡」整块移除：它原来粘在视口底部，删掉后本页不再有第二处保存入口。
	if strings.Contains(body, "article-save-card") {
		t.Error("保存卡应已移除（保存按钮进页头），实际仍渲染出 article-save-card")
	}
	// 右栏三张卡（实时预览 / 发布 / 可视化编辑）：SEO 评测搬进抽屉后不再占栏宽 ——
	// 写文章时一直要看的是正文与预览，评测是按需看的。
	aside := articleSection(t, body, `<aside class="article-edit-side">`, "</aside>")
	if n := strings.Count(aside, `<section class="card`); n != 3 {
		t.Errorf("aside 卡片数应为 3（评测已移入抽屉），实际 %d", n)
	}
	// 反向：评测内容不该再常驻在页面上（否则「移进抽屉」只是多渲染一份）。
	if strings.Contains(aside, `hx-post="/admin/articles/score"`) ||
		strings.Contains(body, `<div id="article-seo-score">`) {
		t.Error("SEO 评测仍常驻在编辑页上：应只有抽屉入口，评测内容与分数容器都在片段里")
	}
	// 评分侧栏必须真的渲染出分数（而不是空态）—— 正文与标题都在，评分应当可用。
	if strings.Contains(body, "评分不可用") {
		t.Errorf("评分侧栏落到了空态：%s", firstArticleBody(data))
	}
}

func TestArticleEditPublishStates(t *testing.T) {
	item := &contentdto.ContentResp{ID: "a1", Slug: "hello-world", Revision: 1, Data: map[string]any{"title": "第一篇"}}

	cases := []struct {
		name    string
		view    gin.H
		want    []string
		notWant []string
	}{
		{
			name: "已发布",
			view: gin.H{"PublishConfigured": true, "Published": true,
				"URLPath": "/blog/hello-world", "PublicURL": "/site/blog/hello-world", "Stale": true},
			want: []string{"/site/blog/hello-world", "内容有更新，产物待重建", "/admin/articles/rebuild",
				// 页头页级动作：保存按钮 + 整块搬来的「重新发布」表单（只有 csrf 与 id）。
				`form="article-form"`, `action="/admin/articles/rebuild"`},
			// 已发布态不该出现任何「发布」入口：页头那个关联按钮与卡内表单在同一条分支里。
			notWant: []string{"/admin/articles/publish", `form="article-publish-form"`},
		},
		{
			name: "可发布（有模板）",
			view: gin.H{"PublishConfigured": true, "Published": false, "HasTemplates": true,
				"DefaultURLPath": "/blog/hello-world",
				"Projects":       []gin.H{{"ID": "p1", "Name": "官网"}},
				"Templates":      []gin.H{{"ID": "t1", "Name": "文章详情", "Version": 2}}},
			want: []string{"/admin/articles/publish", "官网", "文章详情", "/blog/hello-world",
				// 未发布态：三个字段留在卡内，页头只放 form="article-publish-form" 关联按钮
				//（这张表单带 projectId / urlPath / templateId，搬不动）。保存按钮同页头。
				`form="article-publish-form"`, `form="article-form"`},
			notWant: []string{"/admin/articles/rebuild"},
		},
		{
			name: "无模板",
			view: gin.H{"PublishConfigured": true, "Published": false, "HasTemplates": false,
				// NoTemplateHint 在生产路径上是 handler **取词后的成品文案**
				//（article_publish.go：articlePublishHintText(tr, articleNoTemplateHint)）。
				// 这里用同一助手 + 无参 tr 复现那条路径：直接把 key 交给模板，模板只会原样
				// 渲染出 key，于是「页面里有一句人话」这条断言永远找不到目标。
				"NoTemplateHint": articlePublishHintText(articlePublishTr(nil), articleNoTemplateHint)},
			want: []string{"还没有「文章详情模板」"},
			// 没有模板时**不能**渲染发布表单，页头也不能留关联按钮：
			// 那会是一次必然失败的点击（卡内给的是「为什么发不了」的说明）。
			notWant: []string{"/admin/articles/publish", `form="article-publish-form"`},
		},
		{
			name:    "能力未装配",
			view:    gin.H{"PublishConfigured": false, "PublishHint": "发布能力未装配（装配缺陷），本页只显示文章内容。"},
			want:    []string{"发布能力未装配"},
			notWant: []string{"/admin/articles/publish", "/admin/articles/rebuild", `form="article-publish-form"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &articlePageHandle{}
			data := articleEditPageData(context.Background(), h, item, "a1", "", "zh-CN")
			for k, v := range tc.view {
				data[k] = v
			}
			body := renderAdminTemplate(t, "admin/content/article_edit.html", articleLayoutData(data))
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("缺少 %q", want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(body, notWant) {
					t.Errorf("不应出现 %q", notWant)
				}
			}
		})
	}
}

// TestArticleScoreFragmentRenders 评分片段（HTMX 目标）单独渲染。
func TestArticleScoreFragmentRenders(t *testing.T) {
	score := articleScoreViewOf(map[string]any{
		"title": "一篇有标题的文章", "body": "<h2>小标题</h2><p>正文内容</p>",
		"excerpt": "摘要", "focusKeyword": "文章",
	}, "/blog/x", "zh-CN")
	// t 必须给：fragments/seo_score 用 .["t"] 取词，缺 t 时 Jet 静默输出空串
	//（不报错、不 500），下面那条断言看到的会是空 span。
	body := renderAdminTemplate(t, "fragments/seo_score",
		gin.H{"Score": score, "t": func(_, fallback string) string { return fallback }})
	if !strings.Contains(body, "SEO 评分（0-100）") {
		t.Errorf("评分片段未渲染出总分块：%s", body[:min(len(body), 200)])
	}
}

// articleSection 取 body 里 start 到其后第一个 end 之间的片段。
//
// 结构断言（「.page-actions 里有几个按钮」「aside 里有几张卡」）只有落在具体区块内才有意义 ——
// 全页计数会把别处的同名属性一并算进来。锚点找不到直接 Fatal：让 -1 进切片会 panic，
// 而 panic 会把真正的原因（模板被中断截断 / 区块被删）盖掉。
func articleSection(t *testing.T, body, start, end string) string {
	t.Helper()
	i := strings.Index(body, start)
	if i < 0 {
		t.Fatalf("渲染结果里找不到锚点 %q", start)
	}
	rest := body[i:]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("锚点 %q 之后找不到结束标记 %q（整页可能被模板中断截断）", start, end)
	}
	return rest[:j]
}

// firstArticleBody 描述评分数据的状态（失败信息用，避免整页刷屏）。
func firstArticleBody(data gin.H) string {
	sv, ok := data["Score"].(scoreView)
	if !ok {
		return "Score 数据缺失"
	}
	return fmt.Sprintf("Score.OK=%v Total=%d", sv.OK, sv.Total)
}

// TestArticleScorePanelRendersFragment 直接打 handler（而不是绕过它去渲染模板）。
//
// 这个测试是被一个真实缺陷补出来的：handler 里的模板名与模板文件对不上时，
// 响应是「HTTP 200 + 空 body」—— 只测模板本身、不经过 handler 的测试看不见它。
func TestArticleScorePanelRendersFragment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)

	h := &articlePageHandle{}
	engine.POST("/admin/articles/score", h.ArticleScorePanel)

	form := url.Values{
		"title":        {"一篇有标题的文章"},
		"body":         {"<h2>小标题</h2><p>正文内容写长一些，够评分器判断内容长度与结构。</p>"},
		"excerpt":      {"摘要"},
		"focusKeyword": {"文章"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/score", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("评分片段渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.TrimSpace(body) == "" {
		t.Fatal("评分片段响应体为空 —— handler 里的模板名很可能与模板文件对不上")
	}
	if !strings.Contains(body, "SEO 评分（0-100）") {
		t.Errorf("评分片段缺少总分块：%s", body[:min(len(body), 200)])
	}
}

// TestArticleSeoDrawerFragmentRenders 抽屉片段：评测内容与「重新评分」都在这里。
//
// 为什么单独一条：评测从常驻卡搬进抽屉后，编辑页的断言只剩「有入口」——
// 若片段本身接错（hx-include 指错表单、hx-target 指不到容器、片段缺 data-drawer-readonly），
// 用户点开抽屉会看到「加载失败」或按了「重新评分」毫无反应，而页面测试全绿。
func TestArticleSeoDrawerFragmentRenders(t *testing.T) {
	score := scoreView{OK: true, Total: 56, Grade: "D",
		SerpTitle: "标题", SerpURL: "/blog/hello-world", SerpDesc: "描述"}
	body := renderAdminTemplate(t, "admin/content/article_seo_drawer.html", gin.H{
		"Score": score, "IsNew": false,
		"t": func(_, fallback string) string { return fallback },
	})
	for _, want := range []string{
		// 只读抽屉的显式声明（drawer.js 的 fragmentRoot 校验：片段要么含 form，
		// 要么声明 data-drawer-readonly —— 本片段只有一个 htmx 按钮，没有可提交字段）。
		"data-drawer-fragment", "data-drawer-readonly",
		// 跨栏依赖必须原样保留：hx-include 指向**抽屉外面**的表单（htmx 选择器不受 DOM 位置限制），
		// hx-target 指向片段内的分数容器 —— 任一断掉按钮就静默失效。
		`hx-post="/admin/articles/score"`, `hx-include="#article-form"`,
		`hx-target="#article-seo-score"`,
		`<div id="article-seo-score">`,
		"重新评分",
		// 关闭出口（抽屉里的取消按钮）
		"data-drawer-close",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("抽屉片段缺少 %q", want)
		}
	}
	// 抽屉片段不得出现 script / iframe 等（fragmentRoot 会直接拒绝）。
	for _, bad := range []string{"<script", "<iframe", "<template"} {
		if strings.Contains(body, bad) {
			t.Errorf("抽屉片段出现 %q：drawer.js 会判非法", bad)
		}
	}
}
