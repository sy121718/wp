package pipeline

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

func TestLocaleCompileOptions(t *testing.T) {
	opts := LocaleCompileOptions("en-US")
	if len(opts) != 2 {
		t.Fatalf("expected 2 options, got %d", len(opts))
	}
}

func TestHighlightPath(t *testing.T) {

	got := HighlightPath(context.Background(), nil, "", "en-US", "/about")
	if got != "/en/about" {
		t.Fatalf("expected /en/about, got %q", got)
	}
}

func TestAppendContentTranslationDefaultLangSkips(t *testing.T) {
	page := &builder.Page{Root: []*core.Node{{ID: "t", Type: "core.text", Props: json.RawMessage(`{"text":"hello"}`)}}}

	factory := func(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator {
		return i18n.NewContentTranslatorScoped(ctx, projectID, nil, lang, hashes)
	}
	opts, tr, n := AppendContentTranslation(nil, context.Background(), nil, "p1", "zh-CN", page, nil, factory)
	if tr != nil || n != 0 || len(opts) != 0 {
		t.Fatalf("default lang should skip translation: tr=%v n=%d opts=%d", tr, n, len(opts))
	}
}

func TestCompileFoundationContract(t *testing.T) {

	if len(LocaleCompileOptions("zh-CN")) != 2 {
		t.Fatal("LocaleCompileOptions must return language + snapshot")
	}

	if len(ClientAssetOptions()) < 4 {
		t.Fatal("ClientAssetOptions must include enhance/track/ui/css")
	}

	if len(AnalyticsCompileOptions(context.Background(), nil, "")) != 0 {
		t.Fatal("AnalyticsCompileOptions should be empty without project")
	}

	opts, err := SiteCompileOptions(SiteCompilePorts{}, SiteCompileParams{
		Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN", LogicalPath: "/x", CurrentPath: "/x",
	})
	if err != nil || len(opts) < 2 {
		t.Fatalf("SiteCompileOptions must inject project + current path, got %d err=%v", len(opts), err)
	}
}
