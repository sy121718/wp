package feature

// page_bilingual_e2e_test.go — 一页多语言同时在线的端到端证据（多语言 P3）。
//
// 走完整装配链（建页 → 每语言构建 → 每语言发布 → 静态访问面 HTTP 读取），
// 打印可核对证据：page_routes 行、page_publications 行、page_artifacts 行、
// FS 激活链接目标、HTTP 响应、sitemap 语言互指、确定性字节对比。

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

// withLangPrefixFeature 临时开启站点语言前缀（测试结束恢复）。
func withLangPrefixFeature(t *testing.T) {
	t.Helper()
	i18n.SetSiteLangPrefix(true)
	t.Cleanup(func() { i18n.SetSiteLangPrefix(false) })
}

// TestPageBilingualSiteOnline 同一页面 zh-CN + en-US 同时在线，互不干扰。
func TestPageBilingualSiteOnline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefixFeature(t)

	// 语言清单：zh-CN（默认）+ en-US。
	if err := db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, created_at, updated_at) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		projectID, "zh-CN", projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}

	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}

	// 先构建两种语言，再逐个发布（验证暂存指针按语言独立）。
	zhBuild, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 构建失败: %v", err)
	}
	enBuild, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 发布失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 发布失败: %v", err)
	}

	// ---- 证据 1：page_routes 两行 active，各自指向各自语言的产物 ----
	type routeRow struct {
		Path       string
		RouteKind  string
		ArtifactID string
	}
	var routes []routeRow
	if err = db.Raw("SELECT path, route_kind, artifact_id FROM page_routes WHERE project_id = ? AND page_id = ? ORDER BY path",
		projectID, page.ID).Scan(&routes).Error; err != nil {
		t.Fatalf("查询路由失败: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("应有两语言各一行路由，实际 %+v", routes)
	}
	for _, r := range routes {
		if r.RouteKind != "active" {
			t.Fatalf("路由应 active: %+v", r)
		}
		t.Logf("路由行: path=%s kind=%s artifact_id=%s", r.Path, r.RouteKind, r.ArtifactID)
	}
	if routes[0].ArtifactID == routes[1].ArtifactID {
		t.Fatalf("两语言路由不应指向同一产物: %+v", routes)
	}

	// ---- 证据 2：page_publications 每语言一行 ----
	type pubRow struct {
		Lang         string
		ActivePath   string
		ArtifactHash string
	}
	var pubs []pubRow
	if err = db.Raw("SELECT lang, active_path, artifact_hash FROM page_publications WHERE page_id = ? ORDER BY lang", page.ID).
		Scan(&pubs).Error; err != nil {
		t.Fatalf("查询激活状态失败: %v", err)
	}
	if len(pubs) != 2 {
		t.Fatalf("应有两条每语言激活状态，实际 %+v", pubs)
	}
	for _, p := range pubs {
		t.Logf("激活状态: lang=%s active_path=%s artifact_hash=%s", p.Lang, p.ActivePath, p.ArtifactHash)
	}

	// ---- 证据 3：page_artifacts 每语言一行，hash 不同 ----
	type artRow struct {
		Lang         string
		ArtifactHash string
		ArtifactKey  string
	}
	var arts []artRow
	if err = db.Raw("SELECT lang, artifact_hash, artifact_key FROM page_artifacts WHERE page_id = ? ORDER BY lang", page.ID).
		Scan(&arts).Error; err != nil {
		t.Fatalf("查询产物行失败: %v", err)
	}
	if len(arts) != 2 || arts[0].ArtifactHash == arts[1].ArtifactHash {
		t.Fatalf("应两语言各一行且 hash 不同，实际 %+v", arts)
	}
	for _, a := range arts {
		t.Logf("产物行: lang=%s hash=%s key=%s", a.Lang, a.ArtifactHash, a.ArtifactKey)
	}

	// ---- 证据 4：FS 激活链接指向各自语言的产物目录 ----
	root := artifactRootOf(t)
	for _, c := range []struct{ lang, hash string }{
		{"zh-CN", zhBuild.StagedHash},
		{"en-US", enBuild.StagedHash},
	} {
		link := filepath.Join(root, "public", "active", c.lang, "about")
		target, lerr := os.Readlink(link)
		if lerr != nil {
			t.Fatalf("读取激活链接失败 %s: %v", link, lerr)
		}
		if !strings.Contains(target, c.hash) {
			t.Fatalf("激活链接未指向本语言产物: %s -> %s（期望含 %s）", link, target, c.hash)
		}
		t.Logf("激活链接: %s -> %s", link, target)
	}

	// ---- 证据 5：HTTP 访问面两个语言各自 200 ----
	router := gin.New()
	router.StaticFS("/site", http.Dir(filepath.Join(root, "public", "active")))
	for _, lang := range []string{"zh-CN", "en-US"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/site/"+lang+"/about/", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
			t.Fatalf("访问 /site/%s/about/ 失败: code=%d", lang, rec.Code)
		}
		t.Logf("HTTP GET /site/%s/about/ -> %d, %d bytes", lang, rec.Code, rec.Body.Len())
	}

	// ---- 证据 6：产物 head 互指 + sitemap 语言分组 ----
	zhHTML, err := os.ReadFile(filepath.Join(root, "artifacts", zhBuild.StagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取 zh 产物失败: %v", err)
	}
	for _, want := range []string{"hreflang=\"zh-CN\" href=\"/zh-CN/about\"", "hreflang=\"en-US\" href=\"/en-US/about\"", "hreflang=\"x-default\" href=\"/zh-CN/about\""} {
		if !strings.Contains(string(zhHTML), want) {
			t.Fatalf("zh 产物缺少 %s", want)
		}
	}
	sitemap, err := os.ReadFile(filepath.Join(root, "public", "active", "sitemap.xml"))
	if err != nil {
		t.Fatalf("读取 sitemap 失败: %v", err)
	}
	for _, want := range []string{"/zh-CN/about", "/en-US/about", "xhtml:link", "hreflang=\"x-default\""} {
		if !strings.Contains(string(sitemap), want) {
			t.Fatalf("sitemap 缺少 %s: %s", want, sitemap)
		}
	}
	t.Logf("sitemap.xml:\n%s", sitemap)

	// ---- 证据 7：确定性——同输入两次构建字节相同 ----
	first, err := os.ReadFile(filepath.Join(root, "artifacts", zhBuild.StagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	again, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("重复构建失败: %v", err)
	}
	if again.StagedHash != zhBuild.StagedHash {
		t.Fatalf("同输入两次构建 hash 不同: %s vs %s", zhBuild.StagedHash, again.StagedHash)
	}
	second, err := os.ReadFile(filepath.Join(root, "artifacts", again.StagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取二次产物失败: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("同输入两次构建产物字节不同")
	}
	t.Logf("确定性: zh 产物 %d 字节，两次构建字节一致（hash=%s）", len(first), zhBuild.StagedHash)
}
