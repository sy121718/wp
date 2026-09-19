package mailenums

// mail_enums_test.go — 对外文案白名单（MailFacingMessages）与 enums 常量的逐条对账。
//
// 为什么要这条测试：MailFacingMessages 是页面归口助手
//（internal/module/mail/inbound/http/mail_err.go 的 mailErrPageText）的白名单 ——
// **漏写一条**的后果是「该业务错误在页面上显示成一句通用提示」：功能不算坏，但没人会发现，
// 而新增业务文案恰恰是每次改需求都会发生的事；**多写一条**没有任何后果。
// 所以把「本包有多少条对外文案常量」与「白名单里有几条」绑在一起：新增一条 Err* / Msg*
// 常量却忘了进白名单，这里直接变红，而不是等运营看见那句通用提示。
//
// 纯 AST 解析，不依赖数据库 / 装配，毫秒级。样板：admin/enums/admin_enums_test.go。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// messageExclusions 不进白名单的常量 → 理由（必须写明；这张表本身也要能被审计）。
var messageExclusions = map[string]string{
	"ErrInternal": "归口文案本身：它是未命中白名单时的返回值，不是业务文案",
}

// TestMailFacingMessagesCoverAllEnums 本包每个 Err* / Msg* 字符串常量都必须落在白名单里
// （或在 messageExclusions 里写明理由）。
func TestMailFacingMessagesCoverAllEnums(t *testing.T) {
	inWhitelist := make(map[string]bool, len(MailFacingMessages))
	for _, v := range MailFacingMessages {
		inWhitelist[v] = true
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}
	declared := map[string]string{} // 常量值 → 常量名（用于第二段「白名单里不能有错别字」）
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
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Err") && !strings.HasPrefix(name.Name, "Msg") {
						continue
					}
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						t.Fatalf("%s 的 %s 取值失败: %v", file, name.Name, uerr)
					}
					checked++
					declared[value] = name.Name
					if _, excluded := messageExclusions[name.Name]; excluded {
						continue
					}
					if !inWhitelist[value] {
						t.Errorf("%s 的 %s = %q 不在 MailFacingMessages 白名单里：经页面归口助手"+
							"会变成一句通用提示，运营拿不到这条业务提示。"+
							"请把常量加进 MailFacingMessages（若它确实不该对外展示，请在 messageExclusions 写明理由）",
							file, name.Name, value)
					}
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("没有解析到任何 Err* / Msg* 字符串常量 —— 文件路径或解析逻辑写错了（测试会变成空转）")
	}

	// 反向：白名单里的每条值都必须是真的常量值（防手写 key 打错字 —— 那种错永远不会被响应对账发现）。
	for _, v := range MailFacingMessages {
		if _, ok := declared[v]; !ok {
			t.Errorf("MailFacingMessages 里的 %q 不是本包的常量值（key 打错 / 常量被改名了）", v)
		}
	}
}
