package contenthttp

import (
	pageview "go_wp/pkg/view"
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

func articleLayoutData(anyView any) gin.H {
	lang := "zh-CN"
	base := pageview.ViewMap(anyView)
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/articles"

	base["Buttons"] = map[string]any{"content.create": true, "content.update": true, "content.delete": true}
	return base
}

func renderAdminTemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()

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
	data := articleListPageData(list, map[string]string{"a1": "/blog/hello-world"}, articleMsg{})
	body := renderAdminTemplate(t, "admin/content/articles.html", articleLayoutData(data))

	for _, want := range []string{
		"第一篇", "hello-world", "第二篇", "second-post",
		"已发布", "未发布", "/site/blog/hello-world", "/admin/articles/edit?id=a1",

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
	engine.GET("/admin/articles", pageview.Slot(ArticlesPageTemplate, pageview.RenderPage), h.ArticlesPage)

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
	engine.GET("/admin/articles", pageview.Slot(ArticlesPageTemplate, pageview.RenderPage), (&articlePageHandle{contents: contents}).ArticlesPage)

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
	data := articleEditPageData(context.Background(), &articlePageHandle{}, item, "a1", articleMsg{}, "zh-CN")
	body := renderAdminTemplate(t, "admin/content/article_edit.html", articleLayoutData(data))

	for _, want := range []string{
		"第一篇", "hello-world", "摘要一", "关键词",
		"/storage/image/cover.webp",

		"trix-editor", `rich-body-article`, `data-rich-editor="body"`, `name="body"`,
		"/static/js/rich-editor/index.js", "/static/css/rich-editor.css",
		"/admin/articles/update",

		"SEO 评测", `data-drawer-open hx-target="#admin-drawer-body" hx-swap="innerHTML" hx-get="/admin/articles/seo/drawer?id=`,
		"保存",

		"可视化编辑", "article-preview-frame",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑页渲染结果缺少 %q", want)
		}
	}

	for _, gone := range []string{`name="seoTitle"`, `name="seoDescription"`, "SEO 标题", "SEO 描述"} {
		if strings.Contains(body, gone) {
			t.Errorf("SEO 字段已合并，编辑页不该再出现 %q", gone)
		}
	}

	head := articleSection(t, body, `class="page-actions"`, "</header>")
	if n := strings.Count(head, `form="article-form"`); n != 1 {
		t.Errorf(".page-actions 里 form=\"article-form\" 的按钮应为 1 个，实际 %d", n)
	}
	if n := strings.Count(body, `form="article-form"`); n != 1 {
		t.Errorf("全页 form=\"article-form\" 的按钮应为 1 个，实际 %d", n)
	}

	if strings.Contains(body, "article-save-card") {
		t.Error("保存卡应已移除（保存按钮进页头），实际仍渲染出 article-save-card")
	}

	aside := articleSection(t, body, `<aside class="article-edit-side">`, "</aside>")
	if n := strings.Count(aside, `<section class="card`); n != 3 {
		t.Errorf("aside 卡片数应为 3（评测已移入抽屉），实际 %d", n)
	}

	if strings.Contains(aside, `hx-post="/admin/articles/score"`) ||
		strings.Contains(body, `<div id="article-seo-score">`) {
		t.Error("SEO 评测仍常驻在编辑页上：应只有抽屉入口，评测内容与分数容器都在片段里")
	}

	if strings.Contains(body, "评分不可用") {
		t.Errorf("评分侧栏落到了空态：%s", firstArticleBody(data))
	}
}

func TestArticleEditPublishStates(t *testing.T) {
	item := &contentdto.ContentResp{ID: "a1", Slug: "hello-world", Revision: 1, Data: map[string]any{"title": "第一篇"}}

	cases := []struct {
		name string

		view    ArticlePublishBlockResp
		want    []string
		notWant []string
	}{
		{
			name: "已发布",
			view: ArticlePublishBlockResp{PublishConfigured: true, Published: true,
				URLPath: "/blog/hello-world", PublicURL: "/site/blog/hello-world", Stale: true},
			want: []string{"/site/blog/hello-world", "内容有更新，产物待重建", "/admin/articles/rebuild",

				`form="article-form"`, `action="/admin/articles/rebuild"`},

			notWant: []string{"/admin/articles/publish", `form="article-publish-form"`},
		},
		{
			name: "可发布（有模板）",
			view: ArticlePublishBlockResp{PublishConfigured: true, Published: false, HasTemplates: true,
				DefaultURLPath: "/blog/hello-world",
				Projects:       []OptionResp{{ID: "p1", Name: "官网"}},
				Templates:      []TemplateOptionResp{{ID: "t1", Name: "文章详情", Version: 2}}},
			want: []string{"/admin/articles/publish", "官网", "文章详情", "/blog/hello-world",

				`form="article-publish-form"`, `form="article-form"`},
			notWant: []string{"/admin/articles/rebuild"},
		},
		{
			name: "无模板",
			view: ArticlePublishBlockResp{PublishConfigured: true, Published: false, HasTemplates: false,

				NoTemplateHint: articlePublishHintMsg(articleNoTemplateHint)},
			want: []string{"还没有「文章详情模板」"},

			notWant: []string{"/admin/articles/publish", `form="article-publish-form"`},
		},
		{
			name: "能力未装配",

			view:    articlePublishUnavailable(articlePublishHintMsg(articlePublishDepsMissingText)),
			want:    []string{"发布能力未装配"},
			notWant: []string{"/admin/articles/publish", "/admin/articles/rebuild", `form="article-publish-form"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &articlePageHandle{}
			data := articleEditPageData(context.Background(), h, item, "a1", articleMsg{}, "zh-CN")

			data.ArticlePublishBlockResp = tc.view
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

func TestArticleScoreFragmentRenders(t *testing.T) {
	score := articleScoreViewOf(map[string]any{
		"title": "一篇有标题的文章", "body": "<h2>小标题</h2><p>正文内容</p>",
		"excerpt": "摘要", "focusKeyword": "文章",
	}, "/blog/x", "zh-CN")

	body := renderAdminTemplate(t, "fragments/seo_score",
		gin.H{"Score": score, "t": func(_, fallback string) string { return fallback }})
	if !strings.Contains(body, "SEO 评分（0-100）") {
		t.Errorf("评分片段未渲染出总分块：%s", body[:min(len(body), 200)])
	}
}

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

func firstArticleBody(data *ArticleEditPageResp) string {
	sv, ok := data.Score.(scoreView)
	if !ok {
		return "Score 数据缺失"
	}
	return fmt.Sprintf("Score.OK=%v Total=%d", sv.OK, sv.Total)
}

func TestArticleScorePanelRendersFragment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)

	h := &articlePageHandle{}
	engine.POST("/admin/articles/score", pageview.Slot(SeoScoreFragmentTemplate, pageview.RenderBare), h.ArticleScorePanel)

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

func TestArticleSeoDrawerFragmentRenders(t *testing.T) {
	score := scoreView{OK: true, Total: 56, Grade: "D",
		SerpTitle: "标题", SerpURL: "/blog/hello-world", SerpDesc: "描述"}
	body := renderAdminTemplate(t, "admin/content/article_seo_drawer.html", gin.H{
		"Score": score, "IsNew": false,
		"t": func(_, fallback string) string { return fallback },
	})
	for _, want := range []string{

		"data-drawer-fragment", "data-drawer-readonly",

		`hx-post="/admin/articles/score"`, `hx-include="#article-form"`,
		`hx-target="#article-seo-score"`,
		`<div id="article-seo-score">`,
		"重新评分",

		"data-drawer-close",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("抽屉片段缺少 %q", want)
		}
	}

	for _, bad := range []string{"<script", "<iframe", "<template"} {
		if strings.Contains(body, bad) {
			t.Errorf("抽屉片段出现 %q：drawer.js 会判非法", bad)
		}
	}
}
