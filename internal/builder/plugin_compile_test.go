package builder

// 插件组件编译集成测试（docs/06-plugin-system.md §7）：
// plugin.* 节点经 RenderContext.Plugin 取规格 → 模板渲染 + 样式编译，
// 产物与内置组件同一确定性管线。

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/plugincomp"
	"go_wp/internal/templates"
)

// testPluginManifest 最小营销插件（一个组件 + 一段样式）。
const testPluginManifest = `{
  "id": "marketing",
  "name": "营销组件",
  "version": "1.0.0",
  "components": [{
    "name": "campaign_card",
    "label": "活动卡片",
    "template": "campaign_card.jet",
    "props": {
      "title": {"kind": "text", "label": "标题", "default": "夏季大促"},
      "level": {"kind": "select", "label": "等级", "options": ["gold","silver"], "default": "gold"},
      "bgColor": {"kind": "color", "label": "背景色"}
    },
    "styles": {"rules": [{
      "decls": [["display","block"],["border-radius","12px"]],
      "bindings": [{"prop":"background","from":"bgColor"}]
    }]}
  }]
}`

// testPluginTemplate 插件组件模板（{{.V.title}} 走 nodeView.V）。
const testPluginTemplate = `<article class="wp-campaign-card {{ .Classes }}">
  <h3>{{ .V.title }}</h3>
  <span class="level">{{ .V.level }}</span>
</article>`

// mapResolverSpecs 手工构建 PluginResolver（内存 spec，不经 plugin 模块）。
func mapResolverSpecs(specs map[string]*core.PluginComponentSpec) core.PluginResolver {
	return testResolver(specs)
}

type testResolver map[string]*core.PluginComponentSpec

func (m testResolver) LookupPluginComponent(t string) (*core.PluginComponentSpec, bool) {
	s, ok := m[t]
	return s, ok
}

// TestPluginComponentCompile 插件组件端到端编译：HTML 模板 + CSS 样式。
func TestPluginComponentCompile(t *testing.T) {
	manifest, err := plugincomp.ParseManifest([]byte(testPluginManifest))
	if err != nil {
		t.Fatalf("manifest 解析失败: %v", err)
	}
	specs := plugincomp.BuildSpecs(manifest)

	// 插件模板命名空间 FS：components/campaign_card.jet。
	pluginFS := fstest.MapFS{
		"components/campaign_card.jet": {Data: []byte(testPluginTemplate)},
	}
	set, err := templates.NewCompositeSet([]templates.PluginFS{{ID: "marketing", FS: pluginFS}})
	if err != nil {
		t.Fatalf("CompositeSet 构建失败: %v", err)
	}

	// 构造含插件节点的页面（props 含 title/bgColor，缺 level 走 default）。
	page, err := ParsePage([]byte(`{
	  "settings": {"layout": {"mode": "full"}},
	  "root": [{
	    "id": "card-1",
	    "type": "plugin.marketing.campaign_card",
	    "props": {"title": "会员日", "bgColor": "#ff0"}
	  }]
	}`))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if err = ValidatePage(page); err != nil {
		t.Fatalf("ValidatePage: %v", err)
	}
	compiled, err := Compile(page, WithComponentSet(set), WithPluginResolver(mapResolverSpecs(specs)))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html := RenderDocument(compiled)

	// HTML：模板渲染，title 用 props 值，level 用 default。
	if !strings.Contains(html, "会员日") || !strings.Contains(html, "gold") {
		t.Fatalf("模板渲染缺失:\n%s", html)
	}
	// CSS：静态声明 + 绑定（bgColor → background）。
	if !strings.Contains(compiled.CSS, "border-radius: 12px") || !strings.Contains(compiled.CSS, "background: #ff0") {
		t.Fatalf("样式编译缺失:\n%s", compiled.CSS)
	}
	// 节点类（wp-c-{id}）进产物（编辑器桥接依赖）。
	if !strings.Contains(html, "wp-c-card-1") {
		t.Fatalf("节点类缺失:\n%s", html)
	}
}

// TestPluginComponentUnknownProps 未声明 props 键拒绝（白名单夹带防线）。
func TestPluginComponentUnknownProps(t *testing.T) {
	manifest, _ := plugincomp.ParseManifest([]byte(testPluginManifest))
	specs := plugincomp.BuildSpecs(manifest)
	page, _ := ParsePage([]byte(`{
	  "settings": {"layout": {"mode": "full"}},
	  "root": [{"id": "c-1", "type": "plugin.marketing.campaign_card",
	             "props": {"evil": "injected"}}]
	}`))
	_, err := Compile(page, WithComponentSet(mustSet(t)), WithPluginResolver(mapResolverSpecs(specs)))
	if err == nil || !strings.Contains(err.Error(), "未声明") {
		t.Fatalf("未知 props 键应拒绝，got: %v", err)
	}
}

// TestPluginComponentMissingResolver 无插件 resolver 时明确报错（非静默）。
func TestPluginComponentMissingResolver(t *testing.T) {
	page, _ := ParsePage([]byte(`{
	  "settings": {"layout": {"mode": "full"}},
	  "root": [{"id": "c-1", "type": "plugin.marketing.campaign_card", "props": {}}]
	}`))
	_, err := Compile(page, WithComponentSet(mustSet(t)))
	if err == nil || !strings.Contains(err.Error(), "插件解析器") {
		t.Fatalf("缺 resolver 应明确报错，got: %v", err)
	}
}

// TestPluginStyleRejectsInjection 绑定值注入被 style 引擎拒绝（防御深度）。
func TestPluginComponentStyleInjection(t *testing.T) {
	manifest, _ := plugincomp.ParseManifest([]byte(testPluginManifest))
	specs := plugincomp.BuildSpecs(manifest)
	page, _ := ParsePage([]byte(`{
	  "settings": {"layout": {"mode": "full"}},
	  "root": [{"id": "c-1", "type": "plugin.marketing.campaign_card",
	             "props": {"bgColor": "red;background:url(https://evil)"}}]
	}`))
	_, err := Compile(page, WithComponentSet(mustSet(t)), WithPluginResolver(mapResolverSpecs(specs)))
	if err == nil {
		t.Fatalf("注入值应被拒绝")
	}
}

// mustSet 内置 embed Set（插件集成测试仅需模板 Set 存在）。
func mustSet(t *testing.T) *jet.Set {
	t.Helper()
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("embed Set 失败: %v", err)
	}
	return set
}
