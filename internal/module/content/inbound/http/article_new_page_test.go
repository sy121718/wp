package contenthttp

// article_new_page_test.go — 文章新建整页（弃抽屉，对齐商品 /admin/products/new）的冒烟。
//
// 钉住四件事：
//   - admin/article_new.html 的 Jet 语法与数据契约成立，标题 / 路径 / 正文编辑器 /
//     预览容器 / 表单 action 都真的渲染出来；
//   - 列表页的抽屉（tpl-article-create 与 data-drawer-open）清干净了，入口是链接；
//   - 编辑页带实时预览容器与可视化编辑（导入）表单；
//   - GET /admin/articles/new 挂的是 content:create 同款 Casbin 中间件（未登录 401），
//     handler 本身渲染 200。

import (
	"context"
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

// articleNewPageData 新建页渲染数据（与 ArticleNewPage 的 gin.H 同款键集）。
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
		// 左栏表单：字段名与原抽屉逐字一致，action 指向既有 create。
		`action="/admin/articles/create"`, `name="title"`, `name="slug"`,
		// 正文走统一富文本片段：隐藏 input / trix-editor / 扩展入口。
		`data-rich-editor="body"`, "trix-editor", `name="body"`,
		"/static/js/rich-editor/index.js",
		// 右栏：实时预览容器（iframe + 预览脚本）与可视化编辑入口（未保存置灰）。
		"article-live-preview", "live-preview.js", "实时预览", "可视化编辑", "disabled",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("新建页渲染结果缺少 %q", want)
		}
	}
}

func TestArticlesListHasNoCreateDrawer(t *testing.T) {
	list := []*contentdto.ContentResp{}
	data := articleListPageData(list, map[string]string{}, "", "")
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
	data := articleEditPageData(context.Background(), &articlePageHandle{}, item, "a1", "", "", "zh-CN")
	body := renderAdminTemplate(t, "admin/content/article_edit.html", articleLayoutData(data))

	for _, want := range []string{
		"article-live-preview", "live-preview.js", // 实时预览容器与脚本
		"可视化编辑", // 导入卡标题（直白措辞，行为不变：复制而非绑定）
	} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑页渲染结果缺少 %q", want)
		}
	}
}

// TestArticleNewPageHandler 新建页 handler：挂 content:create 同款 Casbin 中间件时
// 未登录请求被 401 挡下（证明中间件真的在链上）；绕过中间件直接进 handler 则 200
// 且渲染出表单。
func TestArticleNewPageHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &articlePageHandle{}
	newEngine := func() *gin.Engine {
		e := gin.New()
		// 模板根目录相对包目录（与 renderAdminTemplate 同款）。
		e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
		return e
	}

	// 直接进 handler：200 + 表单。
	engine := newEngine()
	engine.GET("/admin/articles/new", h.ArticleNewPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/articles/new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("新建页 handler 状态 %d，期望 200", rec.Code)
	}
	for _, want := range []string{`action="/admin/articles/create"`, `name="body"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("新建页缺少 %q", want)
		}
	}

	// 同款中间件链（与 article_router.go 的注册一致）：无 user_id → 401。
	//
	// 注意用的是 CasbinMiddlewareForPathAs 而非 CasbinMiddlewareForPath —— 新建页是 GET
	// 页面，但复用的 /api/content/create 权限点在库里声明的是 POST。取真实请求方法（GET）
	// 去 enforce 会恒不匹配，**含超管在内全员 403**（2026-08 线上缺陷）。
	// 本用例断言「中间件在链上」；动词语义由 TestArticleNewPageCasbinAction 单独钉住。
	guarded := newEngine()
	guarded.GET("/admin/articles/new", builtin.CasbinMiddlewareForPathAs("/api/content/create", http.MethodPost), h.ArticleNewPage)
	rec2 := httptest.NewRecorder()
	guarded.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/admin/articles/new", nil))
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("未登录请求应被 Casbin 中间件 401 挡下，实际 %d", rec2.Code)
	}
}

// TestArticleNewPageCasbinForPathAsValidatesAction 钉住 CasbinMiddlewareForPathAs 的
// 参数校验：非法动词在**装配期** panic，而不是让路由永久 403 到用户撞见为止。
//
// 这是「GET 页面复用 POST 权限点」这个模式的护栏：将来若有人把 http.MethodPost 写成
// "post"（小写，库里策略是大写）或写成 GET，前者当场炸、后者被 TestArticleNewPageHandler
// 的链路用例暴露。
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

	// 合法动词不 panic（含空串 = 回退真实请求方法，与 ForPath 行为一致）。
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
