package builder

import (
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

const navMenuDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "base": {}, "seo": {"title": "导航测试"}},
  "root": [
    {"id": "nav1", "type": "core.nav", "props": {"menu": "header"}}
  ]
}`

const navCustomDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "base": {}, "seo": {"title": "导航测试"}},
  "root": [
    {"id": "nav1", "type": "core.nav", "props": {"items": [{"label": "关于", "url": "/about"}]}}
  ]
}`

type fakeNavResolver struct {
	items   []core.NavigationItem
	err     error
	calls   int
	project string
	kind    string
}

func (f *fakeNavResolver) ResolveMenu(projectID, kind string) ([]core.NavigationItem, error) {
	f.calls++
	f.project, f.kind = projectID, kind
	return f.items, f.err
}

func (f *fakeNavResolver) ResolveNavigation(projectID, navigationID string) ([]core.NavigationItem, error) {
	return nil, nil
}

func compileNavDoc(t *testing.T, doc string, opts ...CompileOption) (*CompiledPage, error) {
	t.Helper()
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("页面文档解析失败: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("组件模板 Set 加载失败: %v", err)
	}
	return Compile(page, append([]CompileOption{WithComponentSet(set)}, opts...)...)
}

func TestNavMenuBindingResolvesAtBuild(t *testing.T) {
	res := &fakeNavResolver{items: []core.NavigationItem{
		{Label: "首页", URL: "/", Target: "self"},
		{Label: "产品", URL: "/products", Children: []core.NavigationItem{
			{Label: "新品", URL: "/new", Target: "blank"},
		}},
	}}
	compiled, err := compileNavDoc(t, navMenuDocJSON, WithNavigationResolver(res), WithProjectID("proj-1"))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if res.calls != 1 || res.project != "proj-1" || res.kind != "header" {
		t.Fatalf("解析器调用参数不符: calls=%d project=%q kind=%q", res.calls, res.project, res.kind)
	}
	for _, want := range []string{`>首页<`, `href="/products"`, `>新品<`, `target="_blank"`} {
		if !strings.Contains(compiled.HTML, want) {
			t.Errorf("产物缺少 %q\n%s", want, compiled.HTML)
		}
	}
}

func TestNavMenuBindingWithoutResolver(t *testing.T) {
	_, err := compileNavDoc(t, navMenuDocJSON, WithProjectID("proj-1"))
	if err == nil || !strings.Contains(err.Error(), "导航") {
		t.Fatalf("缺少解析器应报错，实际: %v", err)
	}
}

func TestNavMenuBindingWithoutProjectID(t *testing.T) {
	res := &fakeNavResolver{}
	_, err := compileNavDoc(t, navMenuDocJSON, WithNavigationResolver(res))
	if err == nil || !strings.Contains(err.Error(), "工程 ID") {
		t.Fatalf("缺少工程 ID 应报错，实际: %v", err)
	}
	if res.calls != 0 {
		t.Errorf("缺少工程 ID 时不应调用解析器，实际调用 %d 次", res.calls)
	}
}

func TestNavMenuBindingResolverError(t *testing.T) {
	res := &fakeNavResolver{err: errors.New("导航数据不可用")}
	_, err := compileNavDoc(t, navMenuDocJSON, WithNavigationResolver(res), WithProjectID("proj-1"))
	if err == nil || !strings.Contains(err.Error(), "导航数据不可用") {
		t.Fatalf("解析失败应透传原因，实际: %v", err)
	}
}

func TestNavCustomItemsNotResolved(t *testing.T) {
	res := &fakeNavResolver{}
	compiled, err := compileNavDoc(t, navCustomDocJSON, WithNavigationResolver(res), WithProjectID("proj-1"))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if res.calls != 0 {
		t.Errorf("未绑定位置不应调用解析器，实际调用 %d 次", res.calls)
	}
	if !strings.Contains(compiled.HTML, `>关于<`) || !strings.Contains(compiled.HTML, `href="/about"`) {
		t.Errorf("手写菜单项未渲染\n%s", compiled.HTML)
	}
}
