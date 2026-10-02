package sysconfigenums

// sysconfig_enums_test.go — 对外文案白名单（SysConfigFacingMessages）与 enums 常量的逐条对账。
//
// 为什么要这条测试：白名单是下一批 inbound 归口助手（sysconfig_err.go）的判据 ——
// **漏写一条**的后果是「该业务错误在页面上显示成一句通用提示」：功能不算坏，但没人会发现，
// 而新增业务文案恰恰是每次改需求都会发生的事；**多写一条**没有后果。所以把「本包有多少条
// 对外错误文案常量」与「白名单里有几条」绑在一起：新增一条 Err* 却忘了进白名单，这里直接变红。
//
// 纯 AST 解析，不依赖数据库 / 装配，毫秒级。形态照 membership/enums 的同名测试。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// messageExclusions 不进白名单的常量 → 理由（新增条目必须写明理由）。
var messageExclusions = map[string]string{
	"MsgGroupSaved":    "成功回执：永远不作为错误响应返回",
	"MsgListSuccess":   "成功回执：永远不作为错误响应返回",
	"MsgDetailSuccess": "成功回执：永远不作为错误响应返回",
	"ErrInternal":      "归口文案本身：它是未命中白名单时的返回值，不是业务文案",
}

// TestSysConfigFacingMessagesCoverAllEnums 本包每个 Err* 常量都必须落在白名单里
// （或在 messageExclusions 里写明理由），且白名单里的值都必须真的是一条常量。
func TestSysConfigFacingMessagesCoverAllEnums(t *testing.T) {
	inWhitelist := make(map[string]bool, len(SysConfigFacingMessages))
	for _, v := range SysConfigFacingMessages {
		inWhitelist[v] = true
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}
	declared := map[string]string{} // 常量值 → 常量名
	checked := 0

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, file, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", file, perr)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) == 0 || len(vs.Values) == 0 {
					continue
				}
				name := vs.Names[0].Name
				lit, ok := vs.Values[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					continue
				}
				declared[val] = name
				if !strings.HasPrefix(name, "Err") {
					continue
				}
				checked++
				if inWhitelist[val] {
					continue
				}
				if _, excluded := messageExclusions[name]; excluded {
					continue
				}
				t.Errorf("常量 %s 是 Err* 却不在 SysConfigFacingMessages 里（补进白名单，或在 messageExclusions 写明理由）", name)
			}
		}
	}

	if checked == 0 {
		t.Fatal("没有解析到任何 Err* 常量：AST 收集逻辑失效，这条门禁会一直绿")
	}
	for _, v := range SysConfigFacingMessages {
		if _, ok := declared[v]; !ok {
			t.Errorf("白名单里的 %q 不是本包的常量值（错别字 / 已删除的常量）", v)
		}
	}
}
