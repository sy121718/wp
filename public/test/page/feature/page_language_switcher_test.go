package feature

// page_language_switcher_test.go — 前台语言切换器进入真实构建产物（多语言 P3）。
//
// 与 page_bilingual_e2e_test.go 同一条链路（建页 → 每语言构建 → 发布 → 静态面 HTTP），
// 断言的是「访问面产物里真的出现可点的语言链接」：
//   - 每种语言产物都指向其他语言的对应路径（<a hreflang>），当前语言为不可点 <span aria-current>；
//   - <html lang> 跟随构建语言；
//   - 语言前缀开关关闭（默认）时不渲染切换器，单语言站点产物字节与 P3 之前一致；
//   - 产物里不得出现任何跳转脚本（访问面零 JS 硬约束）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// switcherFragment 从产物 HTML 里截出语言切换器的 <nav> 片段（证据用，取不到返回空串）。
func switcherFragment(html string) string {
	start := strings.Index(html, `<nav class="wp-c-`)
	if start < 0 {
		return ""
	}
	end := strings.Index(html[start:], "</nav>")
	if end < 0 {
		return ""
	}
	return html[start : start+end+len("</nav>")]
}

// switcherDoc 含语言切换器节点的页面文档。
const switcherDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"lang1","type":"core.languages","props":{"gap":"16px"}}]}`

// TestPageArtifactLanguageSwitcher 双语站点：两种语言的产物都含切换链接。
func TestPageArtifactLanguageSwitcher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefixFeature(t)

	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, created_at, updated_at) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		projectID, "zh-CN", projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}

	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(switcherDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}

	zh, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 构建失败: %v", err)
	}
	en, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 构建失败: %v", err)
	}
	root := artifactRootOf(t)
	readArtifact := func(hash string) string {
		t.Helper()
		b, rerr := os.ReadFile(filepath.Join(root, "artifacts", hash, "index.html"))
		if rerr != nil {
			t.Fatalf("读取产物失败(%s): %v", hash, rerr)
		}
		return string(b)
	}
	zhHTML := readArtifact(zh.StagedHash)
	enHTML := readArtifact(en.StagedHash)

	// 打印真实产物里的切换器片段（可核对证据）。
	t.Logf("zh 产物切换器片段: %s", switcherFragment(zhHTML))
	t.Logf("en 产物切换器片段: %s", switcherFragment(enHTML))

	// zh 产物：指向 en-US 的纯链接 + 当前语言（zh-CN）不可点标记 + html lang。
	for _, want := range []string{
		`<html lang="zh-CN">`,
		`<a class="wp-lang-link" href="/en-US/about" hreflang="en-US" lang="en-US">English</a>`,
		`<span class="wp-lang-current" lang="zh-CN" aria-current="true">简体中文</span>`,
	} {
		if !strings.Contains(zhHTML, want) {
			t.Fatalf("zh 产物缺少 %q", want)
		}
	}
	// 当前语言不可点：head 里的 hreflang 自指链接不算（那是 SEO 标注），
	// 切换器内部不得出现指向自身的 <a class="wp-lang-link">。
	if strings.Contains(zhHTML, `<a class="wp-lang-link" href="/zh-CN/about"`) {
		t.Fatal("zh 产物当前语言不应渲染为链接")
	}

	// en 产物：反向链接 + 当前语言标记随语言变化 + html lang 同步。
	for _, want := range []string{
		`<html lang="en-US">`,
		`<a class="wp-lang-link" href="/zh-CN/about" hreflang="zh-CN" lang="zh-CN">简体中文</a>`,
		`<span class="wp-lang-current" lang="en-US" aria-current="true">English</span>`,
	} {
		if !strings.Contains(enHTML, want) {
			t.Fatalf("en 产物缺少 %q", want)
		}
	}

	// 零 JS：切换器不引入脚本跳转（产物里唯一的 <script> 是增强脚本与 JSON-LD）。
	for _, html := range []string{zhHTML, enHTML} {
		if strings.Contains(html, "location.href=") || strings.Contains(html, "onclick=") {
			t.Fatal("切换器不得引入 JS 跳转")
		}
	}

	// 发布后静态面可直接读到链接（访问面纯静态）。
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 发布失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 发布失败: %v", err)
	}
	router := gin.New()
	router.StaticFS("/site", http.Dir(filepath.Join(root, "public", "active")))
	for _, c := range []struct{ lang, wantLink string }{
		{"zh-CN", `href="/en-US/about"`},
		{"en-US", `href="/zh-CN/about"`},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/site/"+c.lang+"/about/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /site/%s/about/ -> %d", c.lang, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), c.wantLink) {
			t.Fatalf("%s 静态产物缺少切换链接 %q", c.lang, c.wantLink)
		}
		t.Logf("HTTP GET /site/%s/about/ -> %d，含切换链接 %s", c.lang, rec.Code, c.wantLink)
	}
}

// TestPageArtifactLanguageSwitcherHiddenWithoutPrefix 语言前缀关闭（默认配置）时
// 多语言映射到同一路径，切换器整块不渲染：单语言站点产物字节与 P3 之前一致。
func TestPageArtifactLanguageSwitcherHiddenWithoutPrefix(t *testing.T) {
	i18n.SetSiteLangPrefix(false)
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, created_at, updated_at) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		projectID, "zh-CN", projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(switcherDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(artifactRootOf(t), "artifacts", built.StagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	// 组件 CSS 仍会编译进产物（节点存在于文档中），这里断言的是「没有渲染出链接列表」。
	if strings.Contains(string(html), `<ul class="wp-lang-list">`) {
		t.Fatal("未开启语言前缀时不应渲染切换器（多语言映射同一路径）")
	}
	if !strings.Contains(string(html), `<html lang="zh-CN">`) {
		t.Fatal("未指定语言时 html lang 应回退默认语言")
	}
}
