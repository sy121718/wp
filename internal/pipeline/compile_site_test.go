package pipeline

import (
	"context"
	"testing"
)

// TestSiteCompileOptions_Minimal 无可选端口时仍注入工程 ID。
func TestSiteCompileOptions_Minimal(t *testing.T) {
	opts, err := SiteCompileOptions(SiteCompilePorts{}, SiteCompileParams{
		Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Lang 非空时除工程 ID 外还会注入站内链接本地化器（审计 I18N-015）：
	// 作者手填的 /shop 在非默认语言站点上要变成 /en/shop。
	if len(opts) != 2 {
		t.Fatalf("expected 2 options (project + site link), got %d", len(opts))
	}
}

// TestSiteCompileOptions_WithPaths 逻辑路径与当前路径同时注入 alternates 与高亮。
func TestSiteCompileOptions_WithPaths(t *testing.T) {
	// 无 project 服务时 LocaleView 降级为空，不应报错。
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

// TestContentTranslationEnabled 默认语言不接入内容翻译。
func TestContentTranslationEnabled(t *testing.T) {
	if ContentTranslationEnabled(context.Background(), nil, "", "zh-CN") {
		t.Error("空工程 ID 时不应启用内容翻译")
	}
}
