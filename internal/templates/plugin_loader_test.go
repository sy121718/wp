package templates

import (
	"io"
	"testing"
	"testing/fstest"
)

func testPluginFS() fstest.MapFS {
	return fstest.MapFS{
		"components/card.jet":   {Data: []byte("<div>{{ .V.title }}</div>")},
		"evil/../../secret.jet": {Data: []byte("secret")},
	}
}

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

func TestCompositeLoaderUnknownPlugin(t *testing.T) {
	l := newCompositeLoader(mustEmbedLoader(t), []PluginFS{{ID: "marketing", FS: testPluginFS()}})
	if l.Exists("plugin/ghost/card.jet") {
		t.Fatalf("未知插件不应命中")
	}
}

func TestCompositeLoaderPathTraversal(t *testing.T) {
	l := newCompositeLoader(mustEmbedLoader(t), []PluginFS{{ID: "marketing", FS: testPluginFS()}})

	if l.Exists("plugin/marketing/../../secret.jet") {
		t.Fatalf("穿越路径不应命中")
	}
}

func TestCompositeLoaderBuiltinIntact(t *testing.T) {
	l := newCompositeLoader(mustEmbedLoader(t), []PluginFS{{ID: "marketing", FS: testPluginFS()}})
	if !l.Exists("_placeholder.jet") {
		t.Fatalf("内置共享模板 _placeholder.jet 应仍可加载")
	}
}

func mustEmbedLoader(t *testing.T) *embedLoader {
	t.Helper()
	l, err := newEmbeddedComponentLoader()
	if err != nil {
		t.Fatalf("embed loader: %v", err)
	}
	return l
}
