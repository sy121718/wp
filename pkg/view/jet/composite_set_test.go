package jet

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

// 插件模板只在自己的源里找；插件缺资源不得回落到应用同名模板。
func TestCompositeLoaderRoutesPluginPathsOnly(t *testing.T) {
	base := NewFSLoader(fstest.MapFS{
		"core.text.jet": {Data: []byte("base")},
	}, []string{"core.text.jet"})
	loader := NewCompositeLoader(base, []PluginFS{{
		ID: "marketing",
		FS: fstest.MapFS{
			"components/campaign.jet": {Data: []byte("plugin")},
		},
	}})

	if !loader.Exists("plugin/marketing/campaign.jet") {
		t.Fatal("插件自己的模板必须可见")
	}
	if loader.Exists("plugin/marketing/missing.jet") {
		t.Fatal("插件缺资源不能回落到基础 loader")
	}
	if loader.Exists("plugin/unknown/campaign.jet") {
		t.Fatal("未知插件不得命中任何源")
	}
	if !loader.Exists("core.text.jet") {
		t.Fatal("非插件路径仍走基础 loader")
	}

	rc, err := loader.Open("plugin/marketing/campaign.jet")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	buf := make([]byte, 6)
	n, _ := rc.Read(buf)
	if string(buf[:n]) != "plugin" {
		t.Fatalf("打开的不是插件模板：%q", buf[:n])
	}
}

// 缓存按 fingerprint 复用，并且有界：只保留最近两代。
func TestCompositeSetCacheIsBoundedAndKeyed(t *testing.T) {
	ResetCompositeSetCache()
	t.Cleanup(ResetCompositeSetCache)

	base := NewFSLoader(fstest.MapFS{"core.text.jet": {Data: []byte("x")}}, []string{"core.text.jet"})
	exts := []string{"", ".jet"}

	first, err := NewCompositeSetCached("fp-1", base, nil, exts)
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewCompositeSetCached("fp-1", base, nil, exts)
	if err != nil {
		t.Fatal(err)
	}
	if first != again {
		t.Fatal("同一 fingerprint 必须复用同一个 Set")
	}

	for _, fp := range []string{"fp-2", "fp-3"} {
		if _, err := NewCompositeSetCached(fp, base, nil, exts); err != nil {
			t.Fatal(err)
		}
	}
	compositeMu.Lock()
	size := len(compositeCache)
	_, hasOldest := compositeCache["fp-1"]
	compositeMu.Unlock()
	if size != compositeCacheLimit {
		t.Fatalf("缓存应有界为 %d，实际 %d", compositeCacheLimit, size)
	}
	if hasOldest {
		t.Fatal("最旧一代应被淘汰")
	}

	// 空 fingerprint 表示不复用。
	a, err := NewCompositeSetCached("", base, nil, exts)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewCompositeSetCached("", base, nil, exts)
	if a == b {
		t.Fatal("空 fingerprint 不应进入缓存")
	}
}

// 插件源缺失时返回明确错误，不静默给空模板。
func TestCompositeLoaderPluginMissIsAnError(t *testing.T) {
	base := NewFSLoader(fstest.MapFS{}, nil)
	loader := NewCompositeLoader(base, nil)
	if _, err := loader.Open("plugin/none/x.jet"); err == nil {
		t.Fatal("未知插件的模板必须报错")
	}
	if loader.Exists("plugin/none/x.jet") {
		t.Fatal("未知插件不应当作存在")
	}
	_ = fs.ErrNotExist
}
