package unit

// page_lang_unit_test.go — 装配层语言全链路（多语言 P2，docs/06-D §4.1）。
//
// 覆盖：开关开启后（决策 D1 全语言带前缀）路径占用、产物 CanonicalPath、
// Manifest.lang、active_path 与 FS 激活位置全部带 /{lang}/ 前缀；
// 显式请求语言（lang=en-US）走同一链路但落在 /en-US/ 下。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/i18n"
)

// withLangPrefix 临时开启站点语言前缀（默认关闭，测试结束恢复）。
func withLangPrefix(t *testing.T) {
	t.Helper()
	i18n.SetSiteLangPrefix(true)
	t.Cleanup(func() { i18n.SetSiteLangPrefix(false) })
}

// artifactManifest 读取落盘产物的 manifest 关键字段。
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

// TestPageLangPrefixFullChain 开启前缀：占用→构建→发布全链路带语言前缀。
func TestPageLangPrefixFullChain(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	created := createPage(t, svc, projectID, "/about", headingDocument)
	// DB 草稿路径仍是逻辑路径（语言是构建维度，不是内容维度）。
	if created.DraftPath != "/about" {
		t.Fatalf("草稿路径应为逻辑路径: %q", created.DraftPath)
	}

	// 路径占用按默认语言带前缀（siteRoutePath）。
	var reserved string
	if err := db.Table("page_routes").Select("path").
		Where("project_id = ? AND page_id = ? AND route_kind = ?", projectID, created.ID, "reserved").
		Scan(&reserved).Error; err != nil {
		t.Fatalf("查询保留路由失败: %v", err)
	}
	if reserved != "/zh-CN/about" {
		t.Fatalf("保留路由应带语言前缀: %q", reserved)
	}

	// 构建：Manifest 记录语言 + 产物路径带前缀。
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	lang, canonical := artifactManifest(t, built.StagedHash)
	if lang != "zh-CN" || canonical != "/zh-CN/about" {
		t.Fatalf("Manifest 语言/路径错误: lang=%q canonicalPath=%q", lang, canonical)
	}

	// 发布：路由行、active_path 与 FS 激活位置都带前缀。
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	if detail.ActivePath == nil || *detail.ActivePath != "/zh-CN/about" {
		t.Fatalf("active_path 应带语言前缀: %v", detail.ActivePath)
	}
	var activeCount int64
	if err = db.Table("page_routes").Where("project_id = ? AND path = ? AND route_kind = ?", projectID, "/zh-CN/about", "active").Count(&activeCount).Error; err != nil {
		t.Fatalf("查询激活路由失败: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("应存在一条 /zh-CN/about 激活路由，实际 %d", activeCount)
	}
	body, rerr := os.ReadFile(filepath.Join(activeDir(t), "zh-CN", "about", "index.html"))
	if rerr != nil || !strings.Contains(string(body), "<html") {
		t.Fatalf("FS 激活入口不可读: %v", rerr)
	}

	// 真实产物形态（供人工核对与文档记录）：
	//   产物目录 {root}/artifacts/{hash}/{index.html,manifest.json}
	//   激活链接 {root}/public/active/zh-CN/about -> ../../artifacts/{hash}
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	rawManifest, _ := os.ReadFile(filepath.Join(root, "artifacts", built.StagedHash, "manifest.json"))
	link := filepath.Join(activeDir(t), "zh-CN", "about")
	target, lerr := os.Readlink(link)
	if lerr != nil {
		t.Fatalf("读取激活链接失败: %v", lerr)
	}
	t.Logf("产物目录: %s", filepath.Join(root, "artifacts", built.StagedHash))
	t.Logf("激活链接: %s -> %s", link, target)
	t.Logf("manifest: %s", rawManifest)
}

// TestPageLangExplicitRequest 显式语言请求落在 /{lang}/ 下，且与默认语言互不干扰。
func TestPageLangExplicitRequest(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	created := createPage(t, svc, projectID, "/en-page", headingDocument)
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	lang, canonical := artifactManifest(t, built.StagedHash)
	if lang != "en-US" || canonical != "/en-US/en-page" {
		t.Fatalf("Manifest 语言/路径错误: lang=%q canonicalPath=%q", lang, canonical)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	var count int64
	if err = db.Table("page_routes").Where("project_id = ? AND path = ? AND route_kind = ?", projectID, "/en-US/en-page", "active").Count(&count).Error; err != nil {
		t.Fatalf("查询激活路由失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("应存在一条 /en-US/en-page 激活路由，实际 %d", count)
	}
}
