package pipeline

import (
	"context"
	"testing"
)

func TestSiteCompileOptions_Minimal(t *testing.T) {
	opts, err := SiteCompileOptions(SiteCompilePorts{}, SiteCompileParams{
		Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(opts) != 2 {
		t.Fatalf("expected 2 options (project + site link), got %d", len(opts))
	}
}

func TestSiteCompileOptions_WithPaths(t *testing.T) {

	opts, err := SiteCompileOptions(SiteCompilePorts{}, SiteCompileParams{
		Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN",
		LogicalPath: "/about", CurrentPath: "/about",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) < 2 {
		t.Fatalf("expected at least project + current path, got %d", len(opts))
	}
}

func TestContentTranslationEnabled(t *testing.T) {
	if ContentTranslationEnabled(context.Background(), nil, "", "zh-CN") {
		t.Error("空工程 ID 时不应启用内容翻译")
	}
}
