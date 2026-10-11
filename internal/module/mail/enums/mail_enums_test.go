package mailenums

// mail_enums_test.go — 本包文案常量的**形态自检**（key 形状 + 唯一性）。
//
// 这条测试原来干的是另一件事：对账 MailFacingMessages 白名单（「本包每个 Err* / Msg* 常量
// 都必须在白名单里」）。那份白名单已随「控制器不取词」这一批删除 —— 判定不再靠枚举常量，
// 而靠错误的形状（pkg/response.IsBusinessError，见 inbound/http/mail_err.go 的文件头）。
//
// 留下来的是另一件**会静默出错**的事：key 写错或写重。
//
//   · 常量的值就是 sys_i18n 的主键，打错一个字的表现是「这个词条永远取不到 → 页面回落
//     源码里的中文兜底」：默认语言下完全看不出来、不报错、日志干净，只有切到 en-US 才暴露；
//   · 两个常量写成同一个值 = 两条文案共用一个词条，改一条会连带改另一条，而两边谁都不知道。
//
// 所以这里按 AST 逐个取字面量来钉：形状必须是 mail.<段>.<段>，且值互不相同。
// 纯 AST 解析，不依赖数据库 / 装配，毫秒级。
//
// 新增一族文案常量时，把名字前缀登记进 isMessageConst —— 漏登记等于漏检（守卫会空转）。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// mailKeyShape 对外文案 key 的形状：mail. 前缀 + 小写点分（至少两段）。
//
// 点号写成 [.] 而不是反斜杠转义 —— 同一件事，少一层转义就少一个写错的入口。
var mailKeyShape = regexp.MustCompile("^mail[.][a-z][a-zA-Z0-9_]*([.][a-zA-Z0-9_]+)+$")

// mailEnumMinMessageConsts 文案常量的最小条数：低于它说明扫描本身失败了（文件改名 / 解析
// 退化），而不是「真的没有常量」—— 空转的守卫比没有守卫更危险。
const mailEnumMinMessageConsts = 40

// isMessageConst 本包的对外文案常量（值即 i18n key）。
//
// 判据是**名字前缀**而不是「值长得像 key」：Err* / Msg* 是既有约定，Notice* 是
// 「控制器不取词」这批新增的页面级受控提示；TestMail* 是测试邮件的主题与正文词条。
func isMessageConst(name string) bool {
	for _, prefix := range []string{"Err", "Msg", "Notice"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	// 测试邮件那三条：主题与正文是 key；它们的 *Fallback 是中文**原文**（词条缺失时的兜底），
	// 按名字前缀一起收进来会把中文当 key 判失败。
	return strings.HasPrefix(name, "TestMail") && !strings.HasSuffix(name, "Fallback")
}

// TestMailEnumKeysAreWellFormed 本包每个文案常量的值都得是合法且唯一的 mail.* key。
func TestMailEnumKeysAreWellFormed(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}

	seen := map[string]string{} // 值 → 常量名（查重）
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
					if !isMessageConst(name.Name) || i >= len(vs.Values) {
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
					if !mailKeyShape.MatchString(value) {
						t.Errorf("%s 的 %s = %q 不是合法的文案 key（期望 mail.<段>.<段>）："+
							"值就是 sys_i18n 的主键，形状不对时取词永远命中不了，页面只会回落中文兜底",
							file, name.Name, value)
					}
					if prev, dup := seen[value]; dup {
						t.Errorf("%s 的 %s 与 %s 用了同一个 key %q：两条文案共用一个词条，"+
							"改一条会连带改另一条（一条文案一个 key）", file, name.Name, prev, value)
						continue
					}
					seen[value] = name.Name
				}
			}
		}
	}

	if checked < mailEnumMinMessageConsts {
		t.Fatalf("只解析到 %d 个文案常量（阈值 %d）：文件路径或解析逻辑写错了，测试会变成空转",
			checked, mailEnumMinMessageConsts)
	}
}
