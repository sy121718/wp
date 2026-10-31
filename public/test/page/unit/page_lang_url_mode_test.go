package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectdto "go_wp/internal/module/project/dto"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/i18n"
)

func saveLocales(t *testing.T, projects *projectservice.Service, projectID string, items ...projectdto.LocaleItem) {
	t.Helper()
	if _, err := projects.SaveLocales(context.Background(), &projectdto.LocalesSaveReq{
		ProjectID: projectID, Locales: items,
	}); err != nil {
		t.Fatalf("保存语言清单失败: %v", err)
	}
}

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

func TestPageLangHomeIndexPath(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	withDefaultPlain(t)
	saveLocales(t, projects, projectID,
		projectdto.LocaleItem{Lang: "zh-CN", IsDefault: true},
		projectdto.LocaleItem{Lang: "en-US"},
	)
	page := createPage(t, svc, projectID, "/", headingDocument)
	got := reservedPaths(t, db, projectID, page.ID)

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
