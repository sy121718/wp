package contenthttp

import (
	"context"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	contentdto "go_wp/internal/module/content/dto"
	"go_wp/internal/templates"
)

func articleNewPageData() gin.H {
	return gin.H{
		"title": articleNewTitle,
		"menu":  "articles",
		"Form": gin.H{
			"ID": "", "Slug": "", "Title": "", "Body": "",
			"Revision": int64(0), "UpdatedAt": "",
		},
	}
}

func TestArticleNewTemplateRenders(t *testing.T) {
	body := renderAdminTemplate(t, "admin/content/article_new.html", articleLayoutData(articleNewPageData()))

	for _, want := range []string{
		"新建文章",

		`hx-post="/admin/articles/create"`, `name="title"`, `name="slug"`,

		`data-rich-editor="body"`, "trix-editor", `name="body"`,
		"/static/js/rich-editor/index.js",

		"article-live-preview", "live-preview.js", "实时预览", "可视化编辑", "disabled",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("新建页渲染结果缺少 %q", want)
		}
	}
}

func TestArticlesListHasNoCreateDrawer(t *testing.T) {
	list := []*contentdto.ContentResp{}
	data := articleListPageData(list, map[string]string{}, articleMsg{})
	body := renderAdminTemplate(t, "admin/content/articles.html", articleLayoutData(data))

	if !strings.Contains(body, "/admin/articles/new") {
		t.Errorf("列表页缺少新建整页入口 /admin/articles/new")
	}
	for _, leftover := range []string{"tpl-article-create", "data-drawer-open"} {
		if strings.Contains(body, leftover) {
			t.Errorf("列表页仍残留抽屉痕迹 %q", leftover)
		}
	}
}

func TestArticleEditRendersPreviewAndImport(t *testing.T) {
	item := &contentdto.ContentResp{ID: "a1", Slug: "hello-world", Revision: 1,
		Data: map[string]any{"title": "第一篇", "body": "<p>正文</p>"}}
	data := articleEditPageData(context.Background(), &articlePageHandle{}, item, "a1", articleMsg{}, "zh-CN")
	body := renderAdminTemplate(t, "admin/content/article_edit.html", articleLayoutData(data))

	for _, want := range []string{

		"article-preview-frame", "/admin/articles/preview-frame?id=", "刷新预览",
		"可视化编辑",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑页渲染结果缺少 %q", want)
		}
	}
}

func TestArticleNewPageHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &articlePageHandle{}
	newEngine := func() *gin.Engine {
		e := gin.New()

		e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
		return e
	}

	engine := newEngine()
	engine.GET("/admin/articles/new", pageview.Slot(ArticleNewPageTemplate, pageview.RenderPage), h.ArticleNewPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/articles/new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("新建页 handler 状态 %d，期望 200", rec.Code)
	}

	for _, want := range []string{`hx-post="/admin/articles/create"`, `name="body"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("新建页缺少 %q", want)
		}
	}

	guarded := newEngine()
	guarded.GET("/admin/articles/new", builtin.CasbinMiddlewareForPathAs("/api/content/create", http.MethodPost), h.ArticleNewPage)
	rec2 := httptest.NewRecorder()
	guarded.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/admin/articles/new", nil))
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("未登录请求应被 Casbin 中间件 401 挡下，实际 %d", rec2.Code)
	}
}

func TestArticleNewPageCasbinForPathAsValidatesAction(t *testing.T) {
	assertPanics := func(t *testing.T, name, act string) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("%s：非法动词 %q 应 panic，实际没有", name, act)
			}
		}()
		builtin.CasbinMiddlewareForPathAs("/api/content/create", act)
	}

	assertPanics(t, "小写 post", "post")
	assertPanics(t, "空字符串以外的未知动词", "FETCH")

	for _, act := range []string{"", http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("合法动词 %q 不应 panic，实际 %v", act, r)
				}
			}()
			builtin.CasbinMiddlewareForPathAs("/api/content/create", act)
		}()
	}
}
