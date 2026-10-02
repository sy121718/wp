package unit

// page_lang_unit_test.go — 装配层语言全链路（多语言 P2/P3，docs/06-D §4.1/§5 方案 A'）。
//
// 覆盖：
//   - default_plain（默认方案）：默认语言产物路径**无前缀**（/about、/index），
//     非默认语言用**短码**（/en/about）；路径占用、Manifest.CanonicalPath、
//     Manifest.lang、active_path 与 FS 激活位置全部同源；
//   - all_prefix：全语言带短码前缀（/zh/about、/en/about）；
//   - 内部语言维度仍是完整码（Manifest.lang=zh-CN / en-US，数据库 lang 列不变）。

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

// withLangURLMode 切换**全局默认**方案（工程未配置时的兜底；测试结束复位）。
//
// 进程级 setter 已删除：全局默认方案由配置源（sys_config 的 i18n 组）注入，测试走同一
// 入口 ValueLoader 打桩；工程级覆盖（projects.settings.langURLMode）由各用例的工程
// settings 决定 —— 不再有「一次设置影响全进程所有工程」的旁路。
func withLangURLMode(t *testing.T, mode i18n.SiteLangURLMode) {
	t.Helper()
	stubGlobalSiteLangURLMode(t, mode)
}

// stubGlobalSiteLangURLMode 用正式入口给全局默认方案打桩，并在用例结束复位。
func stubGlobalSiteLangURLMode(t *testing.T, mode i18n.SiteLangURLMode) {
	t.Helper()
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{SiteLangURLMode: string(mode)}, nil
	})
	t.Cleanup(func() {
		// 复位到「未注入 = 代码内常量」：先注入零值把已生效的值打回常量，再摘掉 loader。
		i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) { return i18n.RuntimeValues{}, nil })
		i18n.SetValueLoader(nil)
	})
}

// withDefaultPlain 默认语言无前缀 + 非默认语言短码。
func withDefaultPlain(t *testing.T) { withLangURLMode(t, i18n.SiteLangURLModeDefaultPlain) }

// withAllPrefix 全语言带短码前缀。
func withAllPrefix(t *testing.T) { withLangURLMode(t, i18n.SiteLangURLModeAllPrefix) }

// withLangPrefix 兼容旧调用名：现语义 = default_plain（默认语言无前缀 + 非默认短码）。
func withLangPrefix(t *testing.T) { withDefaultPlain(t) }

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

// TestPageLangDefaultPlainFullChain 默认语言全链路无前缀：占用→构建→发布。
func TestPageLangDefaultPlainFullChain(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)

	created := createPage(t, svc, projectID, "/about", headingDocument)
	// DB 草稿路径仍是逻辑路径（语言是构建维度，不是内容维度）。
	if created.DraftPath != "/about" {
		t.Fatalf("草稿路径应为逻辑路径: %q", created.DraftPath)
	}

	// 路径占用：默认语言无前缀。
	var reserved string
	if err := db.Table("page_routes").Select("path").
		Where("project_id = ? AND page_id = ? AND route_kind = ?", projectID, created.ID, "reserved").
		Scan(&reserved).Error; err != nil {
		t.Fatalf("查询保留路由失败: %v", err)
	}
	if reserved != "/about" {
		t.Fatalf("默认语言保留路由不应带前缀: %q", reserved)
	}

	// 构建：Manifest 语言仍是完整码，产物路径无前缀。
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	lang, canonical := artifactManifest(t, built.StagedHash)
	if lang != "zh-CN" || canonical != "/about" {
		t.Fatalf("Manifest 语言/路径错误: lang=%q canonicalPath=%q", lang, canonical)
	}

	// 发布：路由行、active_path 与 FS 激活位置都无前缀。
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

	// 真实产物形态（供人工核对与文档记录）：
	//   产物目录 {root}/artifacts/{hash}/{index.html,manifest.json}
	//   激活链接 {root}/public/active/about -> ../artifacts/{hash}
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

// TestPageLangExplicitRequest 显式请求非默认语言：落在短码前缀下（/en/en-page）。
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

// TestPageLangAllPrefixShortCode all_prefix 方案：全语言带短码前缀。
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
