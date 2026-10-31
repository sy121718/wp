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

func withDefaultPlainFeature(t *testing.T) {
	t.Helper()
	stubGlobalSiteLangURLMode(t, i18n.SiteLangURLModeDefaultPlain)
}

func withLangURLMode(t *testing.T, mode i18n.SiteLangURLMode) {
	t.Helper()
	stubGlobalSiteLangURLMode(t, mode)
}

func stubGlobalSiteLangURLMode(t *testing.T, mode i18n.SiteLangURLMode) {
	t.Helper()
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{SiteLangURLMode: string(mode)}, nil
	})
	t.Cleanup(func() {

		i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) { return i18n.RuntimeValues{}, nil })
		i18n.SetValueLoader(nil)
	})
}

func TestPageBilingualSiteOnline(t *testing.T) {
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
		DraftPath: "/about", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}

	zhBuild, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 构建失败: %v", err)
	}

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

	type artRow struct {
		Lang         string
		ArtifactHash string
		ArtifactKey  string
	}
	var arts []artRow
	if err = db.Raw("SELECT a.lang, a.artifact_hash, a.artifact_key FROM page_artifacts a JOIN page_publications p ON p.artifact_id = a.id WHERE p.page_id = ? ORDER BY a.lang", page.ID).
		Scan(&arts).Error; err != nil {
		t.Fatalf("查询产物行失败: %v", err)
	}
	if len(arts) != 2 || arts[0].ArtifactHash == arts[1].ArtifactHash {
		t.Fatalf("当前发布应两语言各一份产物且 hash 不同，实际 %+v", arts)
	}
	for _, a := range arts {
		t.Logf("产物行: lang=%s hash=%s key=%s", a.Lang, a.ArtifactHash, a.ArtifactKey)
	}

	root := artifactRootOf(t)

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

	rebuilt, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 重建失败: %v", err)
	}
	if err = db.Raw("SELECT a.lang, a.artifact_hash, a.artifact_key FROM page_artifacts a JOIN page_publications p ON p.artifact_id = a.id WHERE p.page_id = ? ORDER BY a.lang", page.ID).
		Scan(&arts).Error; err != nil {
		t.Fatalf("重新查询产物行失败: %v", err)
	}
	for _, a := range arts {
		if activeHash[a.Lang] != a.ArtifactHash {
			t.Fatalf("仅Build改变了当前发布的产物: %s", a.Lang)
		}
	}
	t.Logf("zh 重建后产物: zh=%s en=%s", activeHash["zh-CN"], activeHash["en-US"])

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

	first, err := os.ReadFile(filepath.Join(root, "artifacts", rebuilt.StagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	again, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("重复构建失败: %v", err)
	}

	if again.StagedHash != rebuilt.StagedHash {
		t.Fatalf("同输入两次构建 hash 不同: %s vs %s", rebuilt.StagedHash, again.StagedHash)
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
