package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
)

// TestLocaleCompileOptions 语言 + 词条快照成对注入。
func TestLocaleCompileOptions(t *testing.T) {
	opts := LocaleCompileOptions("en-US")
	if len(opts) != 2 {
		t.Fatalf("expected 2 options, got %d", len(opts))
	}
}

// TestHighlightPath 逻辑路径映射为本语言访问路径。
func TestHighlightPath(t *testing.T) {
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })

	got := HighlightPath(context.Background(), nil, "", "en-US", "/about")
	if got != "/en/about" {
		t.Fatalf("expected /en/about, got %q", got)
	}
}

// TestAppendContentTranslationDefaultLangSkips 默认语言不注入 ContentTranslator。
func TestAppendContentTranslationDefaultLangSkips(t *testing.T) {
	page := &builder.Page{Root: []*core.Node{{ID: "t", Type: "core.text", Props: json.RawMessage(`{"text":"hello"}`)}}}
	// 工厂现在是 4 参（ctx, projectID, lang, hashes）—— 加 projectID 是为了让取词按工程隔离
	// （审计 I18N-009：本工程行优先、未命中回落全局行）。这里用 NewContentTranslatorScoped
	// 把工程作用域接上，而不是传旧的 3 参函数：后者编译不过，且传 nil 的形态会掩盖作用域参数。
	factory := func(ctx context.Context, projectID, lang string, hashes []string) *i18n.ContentTranslator {
		return i18n.NewContentTranslatorScoped(ctx, projectID, nil, lang, hashes)
	}
	opts, tr, n := AppendContentTranslation(nil, context.Background(), nil, "p1", "zh-CN", page, nil, factory)
	if tr != nil || n != 0 || len(opts) != 0 {
		t.Fatalf("default lang should skip translation: tr=%v n=%d opts=%d", tr, n, len(opts))
	}
}

// TestCompileFoundationContract page 与 presentation 必须共用的基础装配项（EDT-003）。
func TestCompileFoundationContract(t *testing.T) {
	// 1) 语言层
	if len(LocaleCompileOptions("zh-CN")) != 2 {
		t.Fatal("LocaleCompileOptions must return language + snapshot")
	}
	// 2) 客户端资源层
	if len(ClientAssetOptions()) < 4 {
		t.Fatal("ClientAssetOptions must include enhance/track/ui/css")
	}
	// 3) 统计代码层（空 ID 时不注入）
	if len(AnalyticsCompileOptions(context.Background(), nil, "")) != 0 {
		t.Fatal("AnalyticsCompileOptions should be empty without project")
	}
	// 3) 站点层（最小）
	opts, err := SiteCompileOptions(SiteCompilePorts{}, SiteCompileParams{
		Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN", LogicalPath: "/x", CurrentPath: "/x",
	})
	if err != nil || len(opts) < 2 {
		t.Fatalf("SiteCompileOptions must inject project + current path, got %d err=%v", len(opts), err)
	}
}
