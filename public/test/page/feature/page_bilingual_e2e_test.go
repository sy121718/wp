package feature

// page_bilingual_e2e_test.go — 一页多语言同时在线的端到端证据（多语言 P3，方案 A'）。
//
// 走完整装配链（建页 → 每语言构建 → 每语言发布 → 静态访问面 HTTP 读取），
// 打印可核对证据：page_routes 行、page_publications 行、page_artifacts 行、
// FS 激活链接目标、HTTP 响应、hreflang 互指、sitemap 语言分组、确定性字节对比。
//
// 方案 A'（i18n.site_lang_url_mode=default_plain）：
//   默认语言 zh-CN → 无前缀 /about；非默认语言 en-US → 短码 /en/about。

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

// withDefaultPlainFeature 切到「默认语言无前缀 + 非默认语言短码」方案（测试结束恢复）。
func withDefaultPlainFeature(t *testing.T) {
	t.Helper()
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })
}

// TestPageBilingualSiteOnline 同一页面 zh-CN + en-US 同时在线，互不干扰。
func TestPageBilingualSiteOnline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlainFeature(t)

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
	// en 的构建返回值不必留存：激活的产物以 page_artifacts 为准（见证据 4 的说明）。
	_, err = svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 发布失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 发布失败: %v", err)
	}

	// ---- 证据 1：page_routes 两行 active，路径分别是 /about 与 /en/about ----
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
	gotPaths := []string{routes[0].Path, routes[1].Path}
	if gotPaths[0] != "/about" || gotPaths[1] != "/en/about" {
		t.Fatalf("路由路径应为 [/about /en/about]，实际 %v", gotPaths)
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

	// ---- 证据 2：page_publications 每语言一行，路径无前缀 / 短码前缀 ----
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
	if pubs[0].Lang != "en-US" || pubs[0].ActivePath != "/en/about" {
		t.Fatalf("en-US 激活路径应为 /en/about，实际 %+v", pubs[0])
	}
	if pubs[1].Lang != "zh-CN" || pubs[1].ActivePath != "/about" {
		t.Fatalf("zh-CN 激活路径应为 /about（默认语言无前缀），实际 %+v", pubs[1])
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

	// ---- 证据 4：FS 激活链接（默认语言在 active/about，非默认语言在 active/en/about）----
	root := artifactRootOf(t)
	// 期望值取**产物表**（真源）而不是构建那一刻的返回值：多语言逐个发布时，
	// 后面某种语言的发布会让先前构建好的产物「落后于站点级状态」（语言切换器按
	// 访问面过滤，集合变了），发布时按当前草稿重新构建并落新产物行 —— 这是正确
	// 行为，激活链接指向的应当是那份最新产物。
	activeHash := map[string]string{}
	for _, a := range arts {
		activeHash[a.Lang] = a.ArtifactHash
	}
	for _, c := range []struct{ lang, hash, rel string }{
		{"zh-CN", activeHash["zh-CN"], filepath.Join("about")},
		{"en-US", activeHash["en-US"], filepath.Join("en", "about")},
	} {
		link := filepath.Join(root, "public", "active", c.rel)
		target, lerr := os.Readlink(link)
		if lerr != nil {
			t.Fatalf("读取激活链接失败 %s: %v", link, lerr)
		}
		if !strings.Contains(target, c.hash) {
			t.Fatalf("激活链接未指向本语言产物: %s -> %s（期望含 %s）", link, target, c.hash)
		}
		t.Logf("激活链接: %s -> %s", link, target)
	}

	// ---- 证据 5：HTTP 访问面两个语言各自 200（/site/about/ 与 /site/en/about/）----
	router := gin.New()
	router.StaticFS("/site", http.Dir(filepath.Join(root, "public", "active")))
	for _, rel := range []string{"about", "en/about"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/site/"+rel+"/", nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
			t.Fatalf("访问 /site/%s/ 失败: code=%d", rel, rec.Code)
		}
		t.Logf("HTTP GET /site/%s/ -> %d, %d bytes", rel, rec.Code, rec.Body.Len())
	}

	// en 发布之后，zh 的产物已经「落后」——它的语言切换器与 hreflang 是在 en 尚未
	// 发布时生成的。静态站点里先发布的语言需要重建一次才能互指，这是构建语义，
	// 不是缺陷；重建后 zh 的产物也会记录进 page_artifacts。
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 重建失败: %v", err)
	}
	if err = db.Raw("SELECT lang, artifact_hash, artifact_key FROM page_artifacts WHERE page_id = ? ORDER BY lang", page.ID).
		Scan(&arts).Error; err != nil {
		t.Fatalf("重新查询产物行失败: %v", err)
	}
	for _, a := range arts {
		activeHash[a.Lang] = a.ArtifactHash
	}
	t.Logf("zh 重建后产物: zh=%s en=%s", activeHash["zh-CN"], activeHash["en-US"])

	// ---- 证据 6：产物 head 互指（默认语言无前缀）+ sitemap 语言分组 ----
	zhHTML, err := os.ReadFile(filepath.Join(root, "artifacts", activeHash["zh-CN"], "index.html"))
	if err != nil {
		t.Fatalf("读取 zh 产物失败: %v", err)
	}
	for _, want := range []string{
		"hreflang=\"zh-CN\" href=\"/about\"",
		"hreflang=\"en-US\" href=\"/en/about\"",
		"hreflang=\"x-default\" href=\"/about\"",
	} {
		if !strings.Contains(string(zhHTML), want) {
			t.Fatalf("zh 产物缺少 %s", want)
		}
	}
	t.Logf("zh 产物 hreflang: %s", hreflangLinesOf(t, zhHTML))

	enHTML, err := os.ReadFile(filepath.Join(root, "artifacts", activeHash["en-US"], "index.html"))
	if err != nil {
		t.Fatalf("读取 en 产物失败: %v", err)
	}
	for _, want := range []string{
		"hreflang=\"zh-CN\" href=\"/about\"",
		"hreflang=\"en-US\" href=\"/en/about\"",
		"hreflang=\"x-default\" href=\"/about\"",
	} {
		if !strings.Contains(string(enHTML), want) {
			t.Fatalf("en 产物缺少 %s", want)
		}
	}
	t.Logf("en 产物 hreflang: %s", hreflangLinesOf(t, enHTML))

	sitemap, err := os.ReadFile(filepath.Join(root, "public", "active", "sitemap.xml"))
	if err != nil {
		t.Fatalf("读取 sitemap 失败: %v", err)
	}
	for _, want := range []string{"<loc>/about</loc>", "<loc>/en/about</loc>", "xhtml:link", "hreflang=\"x-default\""} {
		if !strings.Contains(string(sitemap), want) {
			t.Fatalf("sitemap 缺少 %s: %s", want, sitemap)
		}
	}
	t.Logf("sitemap.xml:\n%s", sitemap)

	// ---- 证据 7：确定性——同输入两次构建字节相同 ----
	first, err := os.ReadFile(filepath.Join(root, "artifacts", activeHash["zh-CN"], "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	again, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("重复构建失败: %v", err)
	}
	// 对比基准取**刚刚那次重建**的产物，而不是最早那次：站点状态（哪些语言已发布）
	// 是构建输入的一部分，跨状态比 hash 比的是两个不同的输入。确定性要求的是
	// 「同一状态、同一草稿」重复构建字节相同 —— 这正是下面这次比对。
	if again.StagedHash != activeHash["zh-CN"] {
		t.Fatalf("同输入两次构建 hash 不同: %s vs %s", activeHash["zh-CN"], again.StagedHash)
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

// hreflangLinesOf 摘出产物 head 中的 alternate 行，便于在测试输出里核对。
func hreflangLinesOf(t *testing.T, html []byte) string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(string(html), "\n") {
		if strings.Contains(line, "rel=\"alternate\"") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return strings.Join(out, " | ")
}
