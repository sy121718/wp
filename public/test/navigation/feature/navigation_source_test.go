package feature

// navigation_source_test.go — 导航来源实体解析（page/article/product/category/block → 标题+URL）。
//
// 覆盖：page 来源解析（标题取 settings.seo.title、URL 取页面路径）、来源实体缺失时
// 回退记录自身的 title/path、解析结果进入编译产物。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	navigationdto "go_wp/internal/module/navigation/dto"
	navsource "go_wp/internal/module/navigation/outbound/source"
	pagedto "go_wp/internal/module/page/dto"
)

// aboutDocument 带 SEO 标题的页面文档（标题由来源解析取用）。
const aboutDocument = `{"settings":{"layout":{"mode":"full"},"seo":{"title":"关于我们"}},"root":[]}`

// TestNavigationSourcePageResolved 页面来源解析为页面标题与路径，并进入产物。
func TestNavigationSourcePageResolved(t *testing.T) {
	_, pages, navSvc, projectID := newNavigationEnv(t)
	ctx := context.Background()

	page, err := pages.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(aboutDocument),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	sourceID := page.ID
	if _, err = navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "占位标题", Path: "/placeholder", Kind: "header",
		SourceType: "page", SourceID: &sourceID,
	}); err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}

	navSvc.SetSourceResolver(navsource.New(pages, nil, nil, nil))
	nodes, err := navSvc.Tree(ctx, projectID, "header")
	if err != nil {
		t.Fatalf("查询菜单树失败: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("菜单项数量不符: %d", len(nodes))
	}
	if nodes[0].Title != "关于我们" {
		t.Errorf("标题未解析为页面 SEO 标题: %q", nodes[0].Title)
	}
	if nodes[0].Path != "/about" {
		t.Errorf("链接未解析为页面路径: %q", nodes[0].Path)
	}

	// 端到端：解析结果经导航组件编译进产物，且当前页被标记高亮。
	html, err := pages.CompilePreview(ctx, []byte(navDocument), projectID, "/about")
	if err != nil {
		t.Fatalf("预览编译失败: %v", err)
	}
	for _, want := range []string{">关于我们<", `href="/about"`, `class="wp-nav-item is-current"`, `aria-current="page"`} {
		if !strings.Contains(string(html), want) {
			t.Errorf("产物缺少 %q", want)
		}
	}
}

// TestNavigationSourceFallbackWhenEntityMissing 来源实体查不到时回退记录自身标题/链接。
func TestNavigationSourceFallbackWhenEntityMissing(t *testing.T) {
	_, pages, navSvc, projectID := newNavigationEnv(t)
	ctx := context.Background()

	missing := "00000000-0000-0000-0000-0000000000cd"
	if _, err := navSvc.Create(ctx, &navigationdto.CreateReq{
		ProjectID: projectID, Title: "占位标题", Path: "/placeholder", Kind: "header",
		SourceType: "page", SourceID: &missing,
	}); err != nil {
		t.Fatalf("创建导航项失败: %v", err)
	}

	navSvc.SetSourceResolver(navsource.New(pages, nil, nil, nil))
	nodes, err := navSvc.Tree(ctx, projectID, "header")
	if err != nil {
		t.Fatalf("查询菜单树失败: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("菜单项数量不符: %d", len(nodes))
	}
	if nodes[0].Title != "占位标题" || nodes[0].Path != "/placeholder" {
		t.Errorf("来源缺失应回退记录自身值，实际: %q %q", nodes[0].Title, nodes[0].Path)
	}
}
