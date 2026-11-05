package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
)

const componentRoot = "components"

// componentRoots 是组件资产的两个真实属主：通用工具组件与业务扩展。
var componentRoots = []string{
	"components",
	"../../internal/builder/extensions",
}

func componentDirs(t *testing.T) []string {
	t.Helper()
	var dirs []string
	for _, root := range componentRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("读取组件目录 %s 失败: %v", root, err)
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == "views" || e.Name() == "presets" {
				continue
			}
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		t.Fatal("组件目录为空（扫描根写错了？）")
	}
	return dirs
}

func TestComponentRegistryComplete(t *testing.T) {
	dirCount := len(componentDirs(t))
	regCount := len(core.Types())
	if regCount != dirCount {
		t.Fatalf("core.Registry 注册数 %d != 组件目录数 %d（漏 import 或多余注册）\n已注册: %v", regCount, dirCount, core.Types())
	}
}

func TestComponentAssetsConsistent(t *testing.T) {
	dirs := componentDirs(t)
	for _, dir := range dirs {
		root := componentRoot
		if _, statErr := os.Stat(filepath.Join(root, dir)); statErr != nil {
			root = componentRoots[1]
		}
		files, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, f := range files {
			name := f.Name()
			path := filepath.Join(root, dir, name)
			switch {
			case strings.HasSuffix(name, ".jet"):

				tpl := strings.TrimSuffix(name, ".jet")
				if _, ok := core.OwnedTemplate(tpl); !ok {
					t.Errorf("%s: 存在 %s 但未注册模板（页面会渲染失败）", dir, name)
				}
			case strings.HasPrefix(name, "enhance") && strings.HasSuffix(name, ".js"):

				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				if !enhanceSourceRegistered(string(body)) {
					t.Errorf("%s: 存在 %s 但没有注册对应的增强块（页面会静默失去交互）", dir, name)
				}
			}
		}
	}

	set, err := templates.NewComponentSet("../../internal/templates/components")
	if err != nil {
		t.Fatalf("构建组件模板集失败: %v", err)
	}
	for _, name := range core.OwnedTemplateNames() {

		if _, err := set.GetTemplate(name); err != nil {
			t.Errorf("注册了模板 %q，但组件目录与集中目录里都没有对应文件: %v", name, err)
		}
	}
}

func enhanceSourceRegistered(src string) bool {
	for _, b := range core.OwnedEnhanceBlocks() {
		if strings.TrimSpace(b.Source) == strings.TrimSpace(src) {
			return true
		}
	}
	return false
}

func TestComponentManifest(t *testing.T) {
	ownedTpl := map[string]bool{}
	for _, n := range core.OwnedTemplateNames() {
		ownedTpl[n] = true
	}
	ownedBlocks := core.OwnedEnhanceBlocks()
	usedBlock := make([]bool, len(ownedBlocks))

	total, withJet, withCss, withEnhance := 0, 0, 0, 0
	var lines []string
	for _, root := range componentRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("读取组件目录 %s 失败: %v", root, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			total++
			dir := e.Name()
			files, _ := os.ReadDir(filepath.Join(root, dir))
			var tags []string
			for _, f := range files {
				n := f.Name()
				switch {
				case strings.HasSuffix(n, ".jet"):
					tags = append(tags, "模板")
					withJet++
				case strings.HasSuffix(n, ".css"):
					tags = append(tags, "样式")
					withCss++
				case strings.HasPrefix(n, "enhance") && strings.HasSuffix(n, ".js"):
					tags = append(tags, "行为")
					withEnhance++
					body, _ := os.ReadFile(filepath.Join(root, dir, n))
					for i, b := range ownedBlocks {
						if strings.TrimSpace(b.Source) == strings.TrimSpace(string(body)) {
							usedBlock[i] = true
						}
					}
				}
			}
			if len(tags) > 0 {
				lines = append(lines, dir+" → "+strings.Join(tags, "/"))
			}
		}
	}

	t.Logf("组件 %d 个；就近资产：模板 %d、样式 %d、行为 %d", total, withJet, withCss, withEnhance)
	for _, l := range lines {
		t.Log("  " + l)
	}
	for i, b := range ownedBlocks {
		if !usedBlock[i] {
			t.Errorf("增强块 %v 已注册但没有对应的组件文件（内容对不上任何一个 enhance*.js）", b.Fns)
		}
	}
}

// componentDirOf 返回组件名所属的扫描根目录。
func componentDirOf(t *testing.T, name string) string {
	t.Helper()
	for _, root := range componentRoots {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.IsDir() {
			return filepath.Join(root, name)
		}
	}
	t.Fatalf("组件 %s 不在任何扫描根里", name)
	return ""
}
