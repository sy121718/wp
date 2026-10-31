package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	navigationdto "go_wp/internal/module/navigation/dto"
	navsource "go_wp/internal/module/navigation/outbound/source"
	pagedto "go_wp/internal/module/page/dto"
)

const aboutDocument = `{"settings":{"layout":{"mode":"full"},"seo":{"title":"关于我们"}},"root":[]}`

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

	html, err := pages.CompilePreview(ctx, []byte(navDocument), projectID, "/about", "", false)
	if err != nil {
		t.Fatalf("预览编译失败: %v", err)
	}
	for _, want := range []string{">关于我们<", `href="/about"`, `class="sky-nav-item is-current"`, `aria-current="page"`} {
		if !strings.Contains(string(html), want) {
			t.Errorf("产物缺少 %q", want)
		}
	}
}

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
