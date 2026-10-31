package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
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

func switcherFragment(html string) string {
	start := strings.Index(html, `<nav class="sky-c-`)
	if start < 0 {
		return ""
	}
	end := strings.Index(html[start:], "</nav>")
	if end < 0 {
		return ""
	}
	return html[start : start+end+len("</nav>")]
}

const switcherDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"lang1","type":"core.languages","props":{"gap":"16px"}}]}`

func TestPageArtifactLanguageSwitcher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlainFeature(t)

	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
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

	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 发布失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 发布失败: %v", err)
	}
	zh, err = svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 重建失败: %v", err)
	}
	en, err = svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 重建失败: %v", err)
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

	t.Logf("zh 产物切换器片段: %s", switcherFragment(zhHTML))
	t.Logf("en 产物切换器片段: %s", switcherFragment(enHTML))

	for _, want := range []string{
		`<html lang="zh-CN">`,
		`<a class="sky-lang-link" href="/en/about" hreflang="en-US" lang="en-US">English</a>`,
		`<span class="sky-lang-current" lang="zh-CN" aria-current="true">简体中文</span>`,
	} {
		if !strings.Contains(zhHTML, want) {
			t.Fatalf("zh 产物缺少 %q", want)
		}
	}

	if strings.Contains(zhHTML, `<a class="sky-lang-link" href="/about"`) {
		t.Fatal("zh 产物当前语言不应渲染为链接")
	}

	for _, want := range []string{
		`<html lang="en-US">`,
		`<a class="sky-lang-link" href="/about" hreflang="zh-CN" lang="zh-CN">简体中文</a>`,
		`<span class="sky-lang-current" lang="en-US" aria-current="true">English</span>`,
	} {
		if !strings.Contains(enHTML, want) {
			t.Fatalf("en 产物缺少 %q", want)
		}
	}

	for _, html := range []string{zhHTML, enHTML} {
		if strings.Contains(html, "location.href=") || strings.Contains(html, "onclick=") {
			t.Fatal("切换器不得引入 JS 跳转")
		}
	}

	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 发布失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 发布失败: %v", err)
	}
	router := gin.New()
	router.StaticFS("/site", http.Dir(filepath.Join(root, "public", "active")))
	for _, c := range []struct{ rel, wantLink string }{
		{"about", `href="/en/about"`},
		{"en/about", `href="/about"`},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/site/"+c.rel+"/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /site/%s/ -> %d", c.rel, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), c.wantLink) {
			t.Fatalf("/site/%s/ 静态产物缺少切换链接 %q", c.rel, c.wantLink)
		}
		t.Logf("HTTP GET /site/%s/ -> %d，含切换链接 %s", c.rel, rec.Code, c.wantLink)
	}
}

func TestPagePreviewLocaleLinksDefaultPlain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlainFeature(t)

	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		projectID, "zh-CN", projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}

	for _, c := range []struct{ lang, want string }{
		{"", `href="/en/about"`},
		{"en-US", `href="/about"`},
	} {
		html, err := svc.CompilePreview(ctx, []byte(switcherDoc), projectID, "/about", c.lang, false)
		if err != nil {
			t.Fatalf("预览编译失败(lang=%q): %v", c.lang, err)
		}
		if !strings.Contains(string(html), c.want) {
			t.Fatalf("预览(lang=%q)缺少 %q\n%s", c.lang, c.want, switcherFragment(string(html)))
		}
		t.Logf("预览 lang=%q 切换器: %s", c.lang, switcherFragment(string(html)))
	}
}

func TestPageArtifactLanguageSwitcherHiddenWithoutPrefix(t *testing.T) {
	stubGlobalSiteLangURLMode(t, i18n.SiteLangURLModeOff)
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
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

	if strings.Contains(string(html), `<ul class="sky-lang-list">`) {
		t.Fatal("未开启语言前缀时不应渲染切换器（多语言映射同一路径）")
	}
	if !strings.Contains(string(html), `<html lang="zh-CN">`) {
		t.Fatal("未指定语言时 html lang 应回退默认语言")
	}
}
