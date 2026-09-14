package dashboardhttp

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

	contentdto "go_wp/internal/module/content/dto"
	"go_wp/internal/templates"
)

// articleLayoutData 补齐 layout 需要的键（与线上 withCSRF/withI18n 注入的一致）。
func articleLayoutData(base gin.H) gin.H {
	lang := "zh-CN"
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/articles"
	return base
}

// renderAdminTemplate 用真实 Jet 渲染器渲染一个后台模板并返回 HTML。
func renderAdminTemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根目录相对包目录：internal/module/dashboard/inbound/http → internal/templates。
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
	data := articleListPageData(list, map[string]string{"a1": "/blog/hello-world"}, "", "")
	body := renderAdminTemplate(t, "admin/articles.html", articleLayoutData(data))

	for _, want := range []string{
		"第一篇", "hello-world", "第二篇", "second-post",
		"已发布", "未发布", "/site/blog/hello-world", "/admin/articles/edit?id=a1",
		"/admin/articles/create", "删除",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页渲染结果缺少 %q", want)
		}
	}
}

func TestArticleEditTemplateRendersSEOFields(t *testing.T) {
	item := &contentdto.ContentResp{ID: "a1", Slug: "hello-world", Revision: 4, UpdatedAt: "2026-09-13 10:00",
		Data: map[string]any{
			"title": "第一篇", "body": "<p>正文</p>", "excerpt": "摘要一",
			"seoTitle": "SEO 标题", "seoDescription": "SEO 描述", "focusKeyword": "关键词",
			"featuredImage": "/storage/image/cover.webp",
		}}
	data := articleEditPageData(context.Background(), &articlePageHandle{}, item, "a1", "", "", "zh-CN")
	body := renderAdminTemplate(t, "admin/article_edit.html", articleLayoutData(data))

	for _, want := range []string{
		"第一篇", "hello-world", "摘要一", "SEO 标题", "SEO 描述", "关键词",
		"/storage/image/cover.webp",
		"trix-editor", "article-body", "/admin/articles/score", "/admin/articles/update",
		"SEO 评测", "重新评分", "保存",
		// 导入到画布区块（06-B 决策 5 的入口）：能力未装配时给提示而不是按钮。
		"导入到画布",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑页渲染结果缺少 %q", want)
		}
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
			want:    []string{"/site/blog/hello-world", "内容有更新，产物待重建", "/admin/articles/rebuild"},
			notWant: []string{"/admin/articles/publish"},
		},
		{
			name: "可发布（有模板）",
			view: gin.H{"PublishConfigured": true, "Published": false, "HasTemplates": true,
				"DefaultURLPath": "/blog/hello-world",
				"Projects":       []gin.H{{"ID": "p1", "Name": "官网"}},
				"Templates":      []gin.H{{"ID": "t1", "Name": "文章详情", "Version": 2}}},
			want:    []string{"/admin/articles/publish", "官网", "文章详情", "/blog/hello-world"},
			notWant: []string{"/admin/articles/rebuild"},
		},
		{
			name: "无模板",
			view: gin.H{"PublishConfigured": true, "Published": false, "HasTemplates": false,
				"NoTemplateHint": articleNoTemplateHint},
			want: []string{"还没有「文章详情模板」"},
			// 没有模板时**不能**渲染发布表单：那会是一次必然失败的点击。
			notWant: []string{"/admin/articles/publish"},
		},
		{
			name:    "能力未装配",
			view:    gin.H{"PublishConfigured": false, "PublishHint": "发布能力未装配（装配缺陷），本页只显示文章内容。"},
			want:    []string{"发布能力未装配"},
			notWant: []string{"/admin/articles/publish", "/admin/articles/rebuild"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &articlePageHandle{}
			data := articleEditPageData(context.Background(), h, item, "a1", "", "", "zh-CN")
			for k, v := range tc.view {
				data[k] = v
			}
			body := renderAdminTemplate(t, "admin/article_edit.html", articleLayoutData(data))
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
	body := renderAdminTemplate(t, "fragments/seo_score", gin.H{"Score": score})
	if !strings.Contains(body, "SEO 评分（0-100）") {
		t.Errorf("评分片段未渲染出总分块：%s", body[:min(len(body), 200)])
	}
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
