package templates

import (
	"io/fs"
	"strings"
	"testing"

	"go_wp/pkg/builder/editor"
)

// 编辑器资产唯一来源：工具层在 pkg/builder/editor，应用只保留宿主适配；
// 不保留旧 workbench 目录镜像，不做逐层回退。
func TestEditorAssetsHaveSingleOwner(t *testing.T) {
	assets, err := StaticAssets()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"js/editor/index.js", "js/editor/catalog.js", "js/editor/methods/canvas.js",
		"js/editor-app/index.js", "css/editor.css", "css/editor-a11y.css",
	} {
		if _, err := fs.Stat(assets, key); err != nil {
			t.Fatalf("复合资产缺少 %s: %v", key, err)
		}
	}
	for _, retired := range []string{
		"js/workbench/index.js", "js/workbench/core.js", "js/workbench/palette.js",
		"css/workbench.css", "css/workbench-a11y.css",
	} {
		if _, err := fs.Stat(assets, retired); err == nil {
			t.Fatalf("复合资产仍包含旧路径 %s（旧目录镜像必须删除，不做回退）", retired)
		}
	}
}

// 工具资产不得出现应用端点与权限语义；业务端点只允许在应用宿主适配层。
func TestEditorToolAssetsContainNoApplicationEndpoints(t *testing.T) {
	assets, err := StaticAssets()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range assets.Keys() {
		if !strings.HasPrefix(key, "js/editor/") || strings.HasSuffix(key, ".test.cjs") {
			continue
		}
		data, err := fs.ReadFile(assets, key)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, bad := range []string{"/api/", "/admin/", "/workbench/"} {
			if strings.Contains(src, bad) {
				t.Errorf("编辑器工具资产 %s 出现应用路径 %q；端点适配应留在 js/editor-app", key, bad)
			}
		}
	}
}

// 版本必须覆盖最终复合 FS（工具 + 应用宿主），不能只哈希工具资产。
func TestStaticJSVersionCoversCompositeAssets(t *testing.T) {
	version := StaticJSVersion()
	if version == "" || version == "0" {
		t.Fatal("StaticJSVersion 未覆盖实际复合资产")
	}
	toolOnly, err := editor.Version(editor.AssetsFS())
	if err != nil {
		t.Fatal(err)
	}
	if toolOnly == version {
		t.Fatal("版本只覆盖工具资产：应用宿主适配变化不会更新缓存键")
	}
}
