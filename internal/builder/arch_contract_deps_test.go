package builder

// arch_contract_deps_test.go — 架构不变量 7 的机械守卫（审计 CQ-001）。
//
// AGENTS.md 不变量 7：共享形状放 internal/builder/source（零依赖，谁都能 import）；
// 业务模块在自己的契约包里声明受限数据源接口；builder/core 直接持有这些契约接口。
// **契约包不得反向 import builder/core** —— 一旦反向即成环（core → 契约 → core），
// core 就再也无法持有业务契约（core/render.go 现在就握着 content / product 的
// 受限数据源接口）。
//
// 这条约束靠人眼守不住：契约包里 import 一个 core 类型是「顺手」的，而后果
// （core 想持有该模块契约时编译直接报 import cycle）要到很久以后才暴露。
// 这里用 AST 扫描把它变成 go test 阶段就红的事 —— 修复方式固定：
// 把形状下沉到 internal/builder/source，core 侧改用别名（见 core/plugin.go）。

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

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/source"
)

// contractRoot 各模块契约包的所在位置（相对本包目录 internal/builder）。
const contractRoot = "../module"

// builderImportPrefix builder 内部包路径前缀（本测试只关心这一族依赖）。
const builderImportPrefix = "go_wp/internal/builder/"

// allowedBuilderImports 契约包允许依赖的 builder 子包白名单：只有零依赖共享形状包。
var allowedBuilderImports = map[string]bool{
	"go_wp/internal/builder/source": true,
}

// exemptedBuilderImports 显式豁免（每一项都要写清理由，豁免不得扩大）。
//
// plugincomp 承载 manifest 的模块间传递形态（plugin 契约里的 ManifestAlias）。
// 它自身依赖 core 与 style，因此这是一条**指向内核的传递依赖**，属待解耦项
// （把 manifest 形状同样下沉到零依赖包即可删除本豁免）。豁免存在的前提是
// ManifestAlias 当前全仓无消费者；一旦它被真正使用，解耦优先级要提上来。
var exemptedBuilderImports = map[string]bool{
	"go_wp/internal/builder/plugincomp": true,
}

// knownContracts 必须被扫到的已知契约包（防「扫描路径写错 → 测试永远绿」）。
var knownContracts = []string{"content", "product", "plugin"}

// pluginShapesStayInSync 编译期钉死：core 与 source 的插件形状是**同一份**。
//
// core.PluginResolver / PluginComponentSpec 是 source 里那批类型/接口的别名，
// 两边的接口必须能互相赋值（方法集等价）—— 一旦有人在 core 侧另起一份同名字状
// 而字段类型不同（比如把 StyleSink 换回 *CSSBuckets），这里立刻编译失败，
// 而不是等到契约包 import core 报环时才发现形状已经分叉。
func pluginShapesStayInSync() {
	var (
		fromSource source.PluginResolver
		fromCore   core.PluginResolver
	)
	fromSource = fromCore // core → source
	fromCore = fromSource // source → core
	_ = fromSource
	_ = fromCore
}

// TestModuleContractPackagesDoNotImportBuilderCore 扫 internal/module 下所有
// contract 包的 import，禁止指向 builder 内核（不变量 7）。
func TestModuleContractPackagesDoNotImportBuilderCore(t *testing.T) {
	dirs, err := contractDirs(contractRoot)
	if err != nil {
		t.Fatalf("遍历契约包目录失败: %v", err)
	}
	if len(dirs) == 0 {
		t.Fatalf("未在 %s 下找到任何 contract 目录（扫描路径可能已失效）", contractRoot)
	}
	// 已知契约包必须在扫描结果里：否则「扫了个空目录」也会让测试通过。
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

// contractDirs 收集 root 下所有名为 contract 的目录（不深入其内部）。
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

// containsString 切片包含判定（小工具，避免本测试引入额外依赖）。
func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
