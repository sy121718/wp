package editor_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/builder/editor"
)

// 独立宿主测试：外部测试包只 import pkg，从非源码工作目录用 embed/FS 打开资产与模板，
// 证明编辑器不需要 internal、config 或私有磁盘目录即可装配。
func TestEditorAssetsAreSelfContained(t *testing.T) {
	// t.Chdir 在用例结束时自动恢复工作目录，避免影响同包其它测试的相对路径。
	t.Chdir(t.TempDir())

	assets := editor.AssetsFS()
	for _, key := range []string{
		"js/editor/index.js", "js/editor/core.js", "js/editor/catalog.js",
		"js/editor/methods/canvas.js", "js/editor/methods/inspector.js",
		"css/editor.css", "css/editor-a11y.css",
	} {
		if _, err := fs.Stat(assets, key); err != nil {
			t.Fatalf("编辑器资产 %s 不存在: %v", key, err)
		}
	}

	version, err := editor.Version(assets)
	if err != nil {
		t.Fatal(err)
	}
	if len(version) != 16 {
		t.Fatalf("内容哈希应为 16 位，实际 %q", version)
	}
	again, err := editor.Version(assets)
	if err != nil || again != version {
		t.Fatalf("同一 FS 的内容哈希应稳定: %q vs %q (%v)", version, again, err)
	}
}

func TestEditorTemplatesLoadWithEmbedOnly(t *testing.T) {
	t.Chdir(t.TempDir())

	templates := editor.TemplatesFS()
	files := []string{
		"editor/workspace.html", "editor/library_panel.html", "editor/edit_panel.html",
		"editor/canvas_stage.html", "editor/navigator.html",
		"fragments/inspector_panel.html", "fragments/outline_tree.html",
	}
	loader := jet.NewOSFileSystemLoader("")
	_ = loader
	set := jet.NewSet(jet.NewInMemLoader(), jet.InDevelopmentMode())
	for _, key := range files {
		src, err := fs.ReadFile(templates, key)
		if err != nil {
			t.Fatalf("编辑器模板 %s 不存在: %v", key, err)
		}
		if _, err := set.Parse(key, string(src)); err != nil {
			t.Fatalf("编辑器模板 %s 解析失败: %v", key, err)
		}
	}
}

func TestEditorHasNoApplicationPaths(t *testing.T) {
	assets := editor.AssetsFS()
	forbidden := []string{"/api/", "/admin/", "/workbench/", "page/draft/save", "confirmDetach"}
	err := fs.WalkDir(assets, "js", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".test.cjs") {
			return err
		}
		data, err := fs.ReadFile(assets, p)
		if err != nil {
			return err
		}
		src := string(data)
		for _, bad := range forbidden {
			if strings.Contains(src, bad) {
				t.Errorf("编辑器工具资产 %s 出现应用路径/语义 %q", p, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEditorTemplateKeysDoNotMirrorAppDirs(t *testing.T) {
	templates := editor.TemplatesFS()
	for _, dir := range []string{"editor", "fragments"} {
		entries, err := fs.ReadDir(templates, dir)
		if err != nil || len(entries) == 0 {
			t.Fatalf("编辑器模板目录 %s 缺失或为空: %v", dir, err)
		}
	}
	if _, err := fs.Stat(templates, "workbench"); err == nil {
		t.Fatal("编辑器工具不应保留应用 workbench 目录镜像")
	}
}
