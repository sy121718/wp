package builder

// 插件组件集合绑定集成测试（docs/06 §9，不变量 4）：
// 组件声明 collection → 构建期经 CollectionResolver 展开列表 → 模板 range 渲染，
// 字段白名单裁剪（未声明字段不得进产物）。

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"go_wp/internal/builder/plugincomp"
	"go_wp/internal/templates"
)

// 集合组件 manifest：product_list 绑定 content:product 集合，渲染 name/price。
const collectionManifest = `{
  "id": "shop",
  "name": "商城组件",
  "version": "1.0.0",
  "components": [{
    "name": "product_list",
    "label": "商品列表",
    "template": "product_list.jet",
    "collection": {"source": "content:product", "fields": ["name", "price"]}
  }]
}`

// 集合模板：range 渲染 .V.items。
const collectionTemplate = `<ul class="{{ .Classes }}">
  {{ range .V.items }}<li>{{ .name }} - {{ .price }}</li>{{ end }}
</ul>`

// fakeCollection 实现 core.CollectionResolver（返回固定列表 + 夹带字段）。
type fakeCollection struct{}

func (fakeCollection) ResolveCollection(_ context.Context, source string, filter map[string]string) ([]map[string]any, error) {
	return []map[string]any{
		{"name": "衬衫", "price": 99.0, "secret": "should-be-cropped"},
		{"name": "裤子", "price": 199.0, "secret": "should-be-cropped"},
	}, nil
}

// TestPluginCollectionRender 集合组件渲染 + 字段白名单裁剪。
func TestPluginCollectionRender(t *testing.T) {
	manifest, err := plugincomp.ParseManifest([]byte(collectionManifest))
	if err != nil {
		t.Fatalf("manifest 解析: %v", err)
	}
	specs := plugincomp.BuildSpecs(manifest)
	pluginFS := fstest.MapFS{"components/product_list.jet": {Data: []byte(collectionTemplate)}}
	set, err := templates.NewCompositeSet([]templates.PluginFS{{ID: "shop", FS: pluginFS}})
	if err != nil {
		t.Fatalf("CompositeSet: %v", err)
	}

	page, _ := ParsePage([]byte(`{
	  "settings": {"layout": {"mode": "full"}},
	  "root": [{"id": "pl-1", "type": "plugin.shop.product_list", "props": {}}]
	}`))
	if err := ValidatePage(page); err != nil {
		t.Fatalf("ValidatePage: %v", err)
	}
	compiled, err := Compile(page,
		WithComponentSet(set),
		WithPluginResolver(mapResolverSpecs(specs)),
		WithCollectionResolver(fakeCollection{}),
	)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	html, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}

	// 列表展开：两条记录 + 字段值。
	if !strings.Contains(html, "衬衫 - 99") || !strings.Contains(html, "裤子 - 199") {
		t.Fatalf("列表未正确展开:\n%s", html)
	}
	// 字段白名单裁剪：secret 不得进产物。
	if strings.Contains(html, "should-be-cropped") || strings.Contains(html, "secret") {
		t.Fatalf("白名单外字段泄漏进产物:\n%s", html)
	}
}

// TestPluginCollectionMissingResolver 无集合解析器时明确报错。
func TestPluginCollectionMissingResolver(t *testing.T) {
	manifest, _ := plugincomp.ParseManifest([]byte(collectionManifest))
	specs := plugincomp.BuildSpecs(manifest)
	pluginFS := fstest.MapFS{"components/product_list.jet": {Data: []byte(collectionTemplate)}}
	set, _ := templates.NewCompositeSet([]templates.PluginFS{{ID: "shop", FS: pluginFS}})

	page, _ := ParsePage([]byte(`{
	  "settings": {"layout": {"mode": "full"}},
	  "root": [{"id": "pl-1", "type": "plugin.shop.product_list", "props": {}}]
	}`))
	_, err := Compile(page,
		WithComponentSet(set),
		WithPluginResolver(mapResolverSpecs(specs)),
	)
	if err == nil || !strings.Contains(err.Error(), "集合解析器") {
		t.Fatalf("缺集合解析器应明确报错: %v", err)
	}
}
