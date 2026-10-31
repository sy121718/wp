package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/i18n"
)

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

func withDefaultPlain(t *testing.T) { withLangURLMode(t, i18n.SiteLangURLModeDefaultPlain) }

func withAllPrefix(t *testing.T) { withLangURLMode(t, i18n.SiteLangURLModeAllPrefix) }

func withLangPrefix(t *testing.T) { withDefaultPlain(t) }

func artifactManifest(t *testing.T, hash string) (lang, canonicalPath string) {
	t.Helper()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	data, err := os.ReadFile(filepath.Join(root, "artifacts", hash, "manifest.json"))
	if err != nil {
		t.Fatalf("读取 manifest 失败: %v", err)
	}
	var m struct {
		Lang          string `json:"lang"`
		CanonicalPath string `json:"canonicalPath"`
	}
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatalf("解析 manifest 失败: %v", err)
	}
	return m.Lang, m.CanonicalPath
}

func TestPageLangDefaultPlainFullChain(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)

	created := createPage(t, svc, projectID, "/about", headingDocument)

	if created.DraftPath != "/about" {
		t.Fatalf("草稿路径应为逻辑路径: %q", created.DraftPath)
	}

	var reserved string
	if err := db.Table("page_routes").Select("path").
		Where("project_id = ? AND page_id = ? AND route_kind = ?", projectID, created.ID, "reserved").
		Scan(&reserved).Error; err != nil {
		t.Fatalf("查询保留路由失败: %v", err)
	}
	if reserved != "/about" {
		t.Fatalf("默认语言保留路由不应带前缀: %q", reserved)
	}

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	lang, canonical := artifactManifest(t, built.StagedHash)
	if lang != "zh-CN" || canonical != "/about" {
		t.Fatalf("Manifest 语言/路径错误: lang=%q canonicalPath=%q", lang, canonical)
	}

	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: created.ID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	if detail.ActivePath == nil || *detail.ActivePath != "/about" {
		t.Fatalf("active_path 应无前缀: %v", detail.ActivePath)
	}
	var activeCount int64
	if err = db.Table("page_routes").Where("project_id = ? AND path = ? AND route_kind = ?", projectID, "/about", "active").Count(&activeCount).Error; err != nil {
		t.Fatalf("查询激活路由失败: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("应存在一条 /about 激活路由，实际 %d", activeCount)
	}
	body, rerr := os.ReadFile(filepath.Join(activeDir(t), "about", "index.html"))
	if rerr != nil || !strings.Contains(string(body), "<html") {
		t.Fatalf("FS 激活入口不可读: %v", rerr)
	}

	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	rawManifest, _ := os.ReadFile(filepath.Join(root, "artifacts", built.StagedHash, "manifest.json"))
	link := filepath.Join(activeDir(t), "about")
	target, lerr := os.Readlink(link)
	if lerr != nil {
		t.Fatalf("读取激活链接失败: %v", lerr)
	}
	t.Logf("产物目录: %s", filepath.Join(root, "artifacts", built.StagedHash))
	t.Logf("激活链接: %s -> %s", link, target)
	t.Logf("manifest: %s", rawManifest)
}

func TestPageLangExplicitRequest(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)

	created := createPage(t, svc, projectID, "/en-page", headingDocument)
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	lang, canonical := artifactManifest(t, built.StagedHash)
	if lang != "en-US" || canonical != "/en/en-page" {
		t.Fatalf("Manifest 语言/路径错误: lang=%q canonicalPath=%q", lang, canonical)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	var count int64
	if err = db.Table("page_routes").Where("project_id = ? AND path = ? AND route_kind = ?", projectID, "/en/en-page", "active").Count(&count).Error; err != nil {
		t.Fatalf("查询激活路由失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("应存在一条 /en/en-page 激活路由，实际 %d", count)
	}
}

func TestPageLangAllPrefixShortCode(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withAllPrefix(t)

	created := createPage(t, svc, projectID, "/about", headingDocument)
	var reserved string
	if err := db.Table("page_routes").Select("path").
		Where("project_id = ? AND page_id = ? AND route_kind = ?", projectID, created.ID, "reserved").
		Scan(&reserved).Error; err != nil {
		t.Fatalf("查询保留路由失败: %v", err)
	}
	if reserved != "/zh/about" {
		t.Fatalf("all_prefix 保留路由应为 /zh/about: %q", reserved)
	}
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	lang, canonical := artifactManifest(t, built.StagedHash)
	if lang != "en-US" || canonical != "/en/about" {
		t.Fatalf("all_prefix Manifest 语言/路径错误: lang=%q canonicalPath=%q", lang, canonical)
	}
}
