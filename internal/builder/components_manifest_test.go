package builder

// components_manifest_test.go — 组件资产清单与一致性检查。
//
// 「一个组件一个目录」落地的过程中，最容易出的问题不是写错代码，而是**资产没被注册**：
// 模板文件放进组件目录却没打 //go:embed 注册、行为脚本写了却没登记特征 ——
// 两者的后果都是**静默失效**（页面看着正常，只是少了功能），且只在用到那个组件时才暴露。
//
// 这个测试把「目录里有什么」与「注册表里有什么」对账，让漂移在 go test 阶段就暴露。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

const componentRoot = "components"

// TestComponentRegistryComplete 组件目录数与 core.Registry 注册数一致（漏 import 即失败）。
func TestComponentRegistryComplete(t *testing.T) {
	entries, err := os.ReadDir(componentRoot)
	if err != nil {
		t.Fatalf("读取组件目录失败: %v", err)
	}
	dirCount := 0
	for _, e := range entries {
		if e.IsDir() {
			dirCount++
		}
	}
	if dirCount == 0 {
		t.Fatal("组件目录为空")
	}
	regCount := len(core.Types())
	if regCount != dirCount {
		t.Fatalf("core.Registry 注册数 %d != 组件目录数 %d（漏 import 或多余注册）\n已注册: %v", regCount, dirCount, core.Types())
	}
}

// TestComponentAssetsConsistent 组件目录里的资产必须与注册表对得上。
func TestComponentAssetsConsistent(t *testing.T) {
	entries, err := os.ReadDir(componentRoot)
	if err != nil {
		t.Fatalf("读取组件目录失败: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		t.Fatal("组件目录为空")
	}

	for _, dir := range dirs {
		files, err := os.ReadDir(filepath.Join(componentRoot, dir))
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, f := range files {
			name := f.Name()
			path := filepath.Join(componentRoot, dir, name)
			switch {
			case strings.HasSuffix(name, ".jet"):
				// 就近模板：文件在，就必须注册 —— 否则 loader 找不到它，构建期报 template not found。
				tpl := strings.TrimSuffix(name, ".jet")
				if _, ok := core.OwnedTemplate(tpl); !ok {
					t.Errorf("%s: 存在 %s 但未注册模板（页面会渲染失败）", dir, name)
				}
			case strings.HasPrefix(name, "enhance") && strings.HasSuffix(name, ".js"):
				// 就近行为：文件在，就必须有某个注册块的源码与它逐字相同。
				// 用内容比对而不是文件名，因为「一个文件一块」是约定而非机制。
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

	// 反向：注册了模板却找不到任何 .jet（既不在组件目录，也不在集中目录）。
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("构建组件模板集失败: %v", err)
	}
	for _, name := range core.OwnedTemplateNames() {
		// GetTemplate 会走 loader（内联覆盖层 + 集中目录），找不到即报错。
		if _, err := set.GetTemplate(name); err != nil {
			t.Errorf("注册了模板 %q，但组件目录与集中目录里都没有对应文件: %v", name, err)
		}
	}
}

// enhanceSourceRegistered 判断某段源码是否已被注册为增强块。
func enhanceSourceRegistered(src string) bool {
	for _, b := range core.OwnedEnhanceBlocks() {
		if strings.TrimSpace(b.Source) == strings.TrimSpace(src) {
			return true
		}
	}
	return false
}

// TestComponentManifest 打印组件清单（go test -run TestComponentManifest -v 查看）。
//
// 清单本身是给人看的：哪个组件带了就近的模板 / 行为。
// 其余组件的资产仍在集中目录，迁移进度由此一目了然。
func TestComponentManifest(t *testing.T) {
	entries, _ := os.ReadDir(componentRoot)
	ownedTpl := map[string]bool{}
	for _, n := range core.OwnedTemplateNames() {
		ownedTpl[n] = true
	}
	ownedBlocks := core.OwnedEnhanceBlocks()
	usedBlock := make([]bool, len(ownedBlocks))

	total, withJet, withCss, withEnhance := 0, 0, 0, 0
	var lines []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		total++
		dir := e.Name()
		files, _ := os.ReadDir(filepath.Join(componentRoot, dir))
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
				body, _ := os.ReadFile(filepath.Join(componentRoot, dir, n))
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
