package templates

// CompositeLoader 命名空间路由测试（docs/06 §7）：插件模板经
// plugin/{pid}/{file} 路由到插件包 components/，内置不受影响，穿越拒绝。

import (
	"io"
	"testing"
	"testing/fstest"
)

// testPluginFS 单个插件包（components/card.jet + 一个上溯路径陷阱）。
func testPluginFS() fstest.MapFS {
	return fstest.MapFS{
		"components/card.jet":   {Data: []byte("<div>{{ .V.title }}</div>")},
		"evil/../../secret.jet": {Data: []byte("secret")},
	}
}

// TestCompositeLoaderRoutesPlugin 插件命名空间路由成功。
func TestCompositeLoaderRoutesPlugin(t *testing.T) {
	l := newCompositeLoader(mustEmbedLoader(t), []PluginFS{{ID: "marketing", FS: testPluginFS()}})

	for _, p := range []string{"plugin/marketing/card.jet", "/plugin/marketing/card.jet"} {
		if !l.Exists(p) {
			t.Fatalf("插件模板 %q 应存在", p)
		}
		rc, err := l.Open(p)
		if err != nil {
			t.Fatalf("Open %q: %v", p, err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if len(b) == 0 {
			t.Fatalf("插件模板内容为空")
		}
	}
}

// TestCompositeLoaderUnknownPlugin 未知插件 ID 回退内置 loader（此处不存在）。
func TestCompositeLoaderUnknownPlugin(t *testing.T) {
	l := newCompositeLoader(mustEmbedLoader(t), []PluginFS{{ID: "marketing", FS: testPluginFS()}})
	if l.Exists("plugin/ghost/card.jet") {
		t.Fatalf("未知插件不应命中")
	}
}

// TestCompositeLoaderPathTraversal 穿越路径拒绝（components/../ 上溯）。
func TestCompositeLoaderPathTraversal(t *testing.T) {
	l := newCompositeLoader(mustEmbedLoader(t), []PluginFS{{ID: "marketing", FS: testPluginFS()}})
	// 规范化后 components 前缀被破坏 → route 返回 false → 回退 base（不存在）。
	if l.Exists("plugin/marketing/../../secret.jet") {
		t.Fatalf("穿越路径不应命中")
	}
}

// TestCompositeLoaderBuiltinIntact 内置模板仍可经 base 加载（命名空间不相交）。
func TestCompositeLoaderBuiltinIntact(t *testing.T) {
	l := newCompositeLoader(mustEmbedLoader(t), []PluginFS{{ID: "marketing", FS: testPluginFS()}})
	if !l.Exists("heading.jet") {
		t.Fatalf("内置模板 heading.jet 应仍可加载")
	}
}

// mustEmbedLoader 内置 embed loader。
func mustEmbedLoader(t *testing.T) *embedLoader {
	t.Helper()
	l, err := newEmbeddedComponentLoader()
	if err != nil {
		t.Fatalf("embed loader: %v", err)
	}
	return l
}
