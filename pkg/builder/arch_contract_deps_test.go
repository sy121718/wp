package builder

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
	"go_wp/pkg/builder/source"
)

const contractRoot = "../../internal/module"

const builderImportPrefix = "go_wp/internal/builder/"

var allowedBuilderImports = map[string]bool{
	"go_wp/pkg/builder/source": true,
}

var exemptedBuilderImports = map[string]bool{}

var knownContracts = []string{"content", "product", "plugin"}

func pluginShapesStayInSync() {
	var (
		fromSource source.PluginResolver
		fromCore   core.PluginResolver
	)
	fromSource = fromCore
	fromCore = fromSource
	_ = fromSource
	_ = fromCore
}

func TestModuleContractPackagesDoNotImportBuilderCore(t *testing.T) {
	dirs, err := contractDirs(contractRoot)
	if err != nil {
		t.Fatalf("遍历契约包目录失败: %v", err)
	}
	if len(dirs) == 0 {
		t.Fatalf("未在 %s 下找到任何 contract 目录（扫描路径可能已失效）", contractRoot)
	}

	for _, mod := range knownContracts {
		want := filepath.Join(contractRoot, mod, "contract")
		if !containsString(dirs, want) {
			t.Fatalf("契约包 %s 未被扫到（扫描路径失效？）", want)
		}
	}

	fset := token.NewFileSet()
	var violations []string
	scanned := 0
	for _, dir := range dirs {
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			t.Fatalf("读取契约包目录失败 %s: %v", dir, rerr)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			af, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if perr != nil {
				t.Fatalf("解析 %s 失败: %v", path, perr)
			}
			scanned++
			for _, imp := range af.Imports {
				dep, uerr := strconv.Unquote(imp.Path.Value)
				if uerr != nil || !strings.HasPrefix(dep, builderImportPrefix) {
					continue
				}
				if allowedBuilderImports[dep] || exemptedBuilderImports[dep] {
					continue
				}
				violations = append(violations, path+" → "+dep)
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("未扫到任何契约包源文件（扫描路径可能已失效）")
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Errorf("契约包反向依赖 builder 内核（AGENTS.md 不变量 7）：\n  %s\n"+
			"修法：把共享形状下沉到 internal/builder/source，core 侧改用类型别名，"+
			"契约包只 import source。", strings.Join(violations, "\n  "))
	}
}

func contractDirs(root string) (dirs []string, err error) {
	werr := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if !d.IsDir() || d.Name() != "contract" {
			return nil
		}
		dirs = append(dirs, path)
		return filepath.SkipDir
	})
	sort.Strings(dirs)
	return dirs, werr
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
