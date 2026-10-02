package routers

// wiring_coverage_test.go — 防复发：`internal/routers` 下所有装配期 `Set*` 注入
// 都必须在 wiringManifest 里有条目（FIX-25 的根因就是「注入了但没进盘点表」）。
//
// 判据不看注释、只看调用：扫描本包（非测试）源码里的 `xxx.SetFoo(...)` 调用，
// 取出方法名与 manifest 的端口值（`模块.SetFoo` 的最后一段）比对。
// 未登记 = 这个注入点漏接时既不 fail-fast 也不进降级日志 —— 正是要消灭的空洞。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// uninjectableSetters 不属于「装配期端口注入」的 Set* 调用 → 理由。
//
// 只列真正不适用清单（不是尚未登记的端口）：新增条目必须写明理由。
var uninjectableSetters = map[string]string{
	// gin 上下文写值（CSRF / 语言等），不是跨模块端口。
	"Set": "gin.Context.Set（请求上下文写值）",
	// 装配期内部状态写入（无「未注入」语义：调用恒发生且不跨模块）。
	"SetStaleMarkers": "media 的多路注入由 media 自身在两处分别登记（portMediaStaleMarker*）",
}

func TestEverySetterCallIsInWiringManifest(t *testing.T) {
	// 判定用**子串包含**：manifest 里存在合并条目（如
	// "presentation.SetNavigationService/SetSitePageResolver/SetMediaProbe/SetPluginService"、
	// "pipeline.Fanout.SetRebuilder(page)"），它们用一条记录覆盖同一次注入的多个 setter。
	known := make([]string, 0, len(wiringManifest))
	for _, e := range wiringManifest {
		known = append(known, e.Port)
	}
	isKnown := func(name string) bool {
		for _, port := range known {
			if strings.Contains(port, name) {
				return true
			}
		}
		return false
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}
	fset := token.NewFileSet()
	found := map[string]string{} // setter 名 → 首次出现的文件
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", f, perr)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name := sel.Sel.Name
			// Setup* 是模块装配入口（不是端口注入）；Set 本身是 gin 的上下文写值。
			if !strings.HasPrefix(name, "Set") || strings.HasPrefix(name, "Setup") || len(name) <= 3 {
				return true
			}
			if _, seen := found[name]; !seen {
				found[name] = f
			}
			return true
		})
	}

	var unregistered []string
	for name, file := range found {
		if isKnown(name) {
			continue
		}
		if _, ok := uninjectableSetters[name]; ok {
			continue
		}
		unregistered = append(unregistered, name+" ("+file+")")
	}
	sort.Strings(unregistered)
	if len(unregistered) > 0 {
		t.Fatalf("以下 Set* 调用不在 wiringManifest 里（漏接时既不 fail-fast 也不进降级日志）：\n  %s\n"+
			"补 manifest 条目 + marks.mark，或加进 uninjectableSetters 并写明理由",
			strings.Join(unregistered, "\n  "))
	}
	t.Logf("已核对 %d 个 Set* 调用；manifest 端口 %d 条", len(found), len(wiringManifest))
}
