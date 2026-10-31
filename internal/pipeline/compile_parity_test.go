package pipeline

import (
	"context"
	"testing"
)

func TestDocumentCompileSharedLayers(t *testing.T) {
	ctx := context.Background()
	lang := "zh-CN"
	projectID := "proj-1"

	if len(LocaleCompileOptions(lang)) != 2 {
		t.Fatal("LocaleCompileOptions 应返回语言 + 快照两项")
	}
	if len(ClientAssetOptions()) == 0 {
		t.Fatal("ClientAssetOptions 不应为空")
	}
	_ = AnalyticsCompileOptions(ctx, nil, projectID)

	set, pluginOpts, err := ComponentSetWithPlugins(nil)
	if err != nil || set == nil {
		t.Fatalf("ComponentSetWithPlugins(nil): set=%v err=%v", set, err)
	}
	if len(pluginOpts) != 0 {
		t.Fatalf("无插件时不应附加 plugin opts")
	}

	siteOpts, err := SiteCompileOptions(SiteCompilePorts{}, SiteCompileParams{
		Ctx: ctx, ProjectID: projectID, Lang: lang,
		LogicalPath: "/products/x", CurrentPath: "/products/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(siteOpts) < 2 {
		t.Fatalf("site opts 过少: %d", len(siteOpts))
	}
}
