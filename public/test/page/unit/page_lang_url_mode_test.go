package unit

// page_lang_url_mode_test.go — 语言 URL 方案的三条装配层防线（docs/06-D §5 方案 A'）。
//
// 覆盖：
//  1. 短码冲突 fail-fast：两种启用语言映射到同一 URL 段时，建页即失败（不静默覆盖）；
//  2. 短码映射可配置：i18n.lang_url_codes 覆盖内置表，产物路径随之变化；
//  3. 默认语言首页：语言根 "/" 映射为 "/index"（默认语言无前缀、非默认语言 /{code}/index）。

import (
	"context"
	"encoding/json"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectdto "go_wp/internal/module/project/dto"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/i18n"
)

// saveLocales 保存站点语言清单（默认语言在前）。
func saveLocales(t *testing.T, projects *projectservice.Service, projectID string, items ...projectdto.LocaleItem) {
	t.Helper()
	if _, err := projects.SaveLocales(context.Background(), &projectdto.LocalesSaveReq{
		ProjectID: projectID, Locales: items,
	}); err != nil {
		t.Fatalf("保存语言清单失败: %v", err)
	}
}

// TestPageLangShortCodeConflictFailsFast zh-TW 与 zh-Hant 都映射到 zh-tw：
// 建页必须在登记路由前失败，绝不静默让两者共用一个访问路径。
func TestPageLangShortCodeConflictFailsFast(t *testing.T) {
	_, svc, projects, projectID := newPageService(t)
	withDefaultPlain(t)
	saveLocales(t, projects, projectID,
		projectdto.LocaleItem{Lang: "zh-CN", IsDefault: true},
		projectdto.LocaleItem{Lang: "zh-TW"},
		projectdto.LocaleItem{Lang: "zh-Hant"},
	)
	_, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(headingDocument),
	})
	if err == nil {
		t.Fatal("短码冲突时建页应失败")
	}
	if err.Error() != pageenums.ErrInvalidPath {
		t.Fatalf("短码冲突应归一为 %q，实际: %v", pageenums.ErrInvalidPath, err)
	}
	t.Logf("短码冲突报错: %v", err)
}

// TestPageLangURLCodesOverride 配置覆盖短码：zh-TW → tw，产物路径随之变化。
func TestPageLangURLCodesOverride(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	withDefaultPlain(t)
	i18n.SetURLCodeOverrides(map[string]string{"zh-TW": "tw"})
	t.Cleanup(func() { i18n.SetURLCodeOverrides(nil) })

	saveLocales(t, projects, projectID,
		projectdto.LocaleItem{Lang: "zh-CN", IsDefault: true},
		projectdto.LocaleItem{Lang: "zh-TW"},
	)
	page := createPage(t, svc, projectID, "/about", headingDocument)
	got := reservedPaths(t, db, projectID, page.ID)
	want := []string{"/about", "/tw/about"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("短码覆盖后应登记 %v，实际 %v", want, got)
	}
	built, err := svc.Build(context.Background(), &pagedto.BuildReq{ID: page.ID, Lang: "zh-TW"})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	lang, canonical := artifactManifest(t, built.StagedHash)
	if lang != "zh-TW" || canonical != "/tw/about" {
		t.Fatalf("Manifest 应为 lang=zh-TW canonicalPath=/tw/about，实际 lang=%q path=%q", lang, canonical)
	}
}

// TestPageLangHomeIndexPath 首页（逻辑路径 "/"）在 default_plain 下映射：
// 默认语言 /index、非默认语言 /en/index。
func TestPageLangHomeIndexPath(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	withDefaultPlain(t)
	saveLocales(t, projects, projectID,
		projectdto.LocaleItem{Lang: "zh-CN", IsDefault: true},
		projectdto.LocaleItem{Lang: "en-US"},
	)
	page := createPage(t, svc, projectID, "/", headingDocument)
	got := reservedPaths(t, db, projectID, page.ID)
	// 首页登记的是 "/"（而不是 "/index"）：路径归一化收敛到 pkg/pathkit 之后
	// （审计 CQ-012），`/index` 与 `/index.html` 都归一为根路径 —— 否则同一个首页会
	// 以两种写法各占一行路由，占用判断与线上内容会分裂。
	// 默认语言的首页是 "/"，非默认语言仍是带前缀的 /en/index。
	want := []string{"/", "/en/index"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("首页应登记 %v，实际 %v", want, got)
	}
	built, err := svc.Build(context.Background(), &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	lang, canonical := artifactManifest(t, built.StagedHash)
	if lang != "en-US" || canonical != "/en/index" {
		t.Fatalf("Manifest 应为 lang=en-US canonicalPath=/en/index，实际 lang=%q path=%q", lang, canonical)
	}
}
