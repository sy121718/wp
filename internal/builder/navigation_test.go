// navigation_test.go — core.nav 菜单位置绑定的构建期解析测试。
//
// 覆盖 docs/09 §2 的内核契约：导航节点绑定 header/footer 时，构建期经
// NavigationResolver 把导航记录展开为静态菜单项；未注入解析器/工程 ID 时
// 编译期显式失败（不静默产出空菜单）；未绑定位置时手写 Items 不受影响。
package builder

import (
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// navMenuDocJSON 绑定菜单位置的导航节点文档（无手写 items）。
const navMenuDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "base": {}, "seo": {"title": "导航测试"}},
  "root": [
    {"id": "nav1", "type": "core.nav", "props": {"menu": "header"}}
  ]
}`

// navCustomDocJSON 手写菜单项的导航节点文档（menu 为空）。
const navCustomDocJSON = `{
  "settings": {"layout": {"mode": "full"}, "base": {}, "seo": {"title": "导航测试"}},
  "root": [
    {"id": "nav1", "type": "core.nav", "props": {"items": [{"label": "关于", "url": "/about"}]}}
  ]
}`

// fakeNavResolver 记录调用参数并返回固定菜单项树。
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

// ResolveNavigation 按项解析：本文件只覆盖「按位置取菜单」，按项路径由
// public/test/navigation/feature 的 navigation_byid_panel_test.go 覆盖。
func (f *fakeNavResolver) ResolveNavigation(projectID, navigationID string) ([]core.NavigationItem, error) {
	return nil, nil
}

// compileNavDoc 编译导航文档（注入组件模板 Set）。
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

// TestNavMenuBindingResolvesAtBuild 绑定菜单位置时用解析结果覆盖 Items。
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

// TestNavMenuBindingWithoutResolver 未注入解析器时编译期显式失败。
func TestNavMenuBindingWithoutResolver(t *testing.T) {
	_, err := compileNavDoc(t, navMenuDocJSON, WithProjectID("proj-1"))
	if err == nil || !strings.Contains(err.Error(), "导航") {
		t.Fatalf("缺少解析器应报错，实际: %v", err)
	}
}

// TestNavMenuBindingWithoutProjectID 缺少工程 ID 时编译期显式失败。
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

// TestNavMenuBindingResolverError 解析失败时编译失败（不产出坏产物）。
func TestNavMenuBindingResolverError(t *testing.T) {
	res := &fakeNavResolver{err: errors.New("导航数据不可用")}
	_, err := compileNavDoc(t, navMenuDocJSON, WithNavigationResolver(res), WithProjectID("proj-1"))
	if err == nil || !strings.Contains(err.Error(), "导航数据不可用") {
		t.Fatalf("解析失败应透传原因，实际: %v", err)
	}
}

// TestNavCustomItemsNotResolved 未绑定位置时手写 Items 生效且不触发解析器。
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
