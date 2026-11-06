package mailenums

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

// mailFlatKeyShape 是根规则要求的词条形状：首字符为字母、仅 ASCII 字母数字、长度 1–15。
var mailFlatKeyShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,14}$`)

// mailLegacyDottedKeys 是本批之前留下的点分存量 key 台账。
// 台账只减不增：新词条一律用扁平短 key，存量改成短 key 时同步删掉这里的条目。
var mailLegacyDottedKeys = map[string]bool{
	"mail.msg.sendSuccess": true,
	"mail.msg.saveSuccess": true,
	"mail.msg.deleteSuccess": true,
	"mail.msg.importSuccess": true,
	"mail.msg.testSent": true,
	"mail.msg.campaignStarted": true,
	"mail.msg.automationStarted": true,
	"mail.err.invalidParam": true,
	"mail.err.accountNotFound": true,
	"mail.err.templateNotFound": true,
	"mail.err.contactNotFound": true,
	"mail.err.campaignNotFound": true,
	"mail.err.accountDisabled": true,
	"mail.err.accountIncomplete": true,
	"mail.err.suppressed": true,
	"mail.err.emailRequired": true,
	"mail.err.emailInvalid": true,
	"mail.err.contactEmailExists": true,
	"mail.err.contactTagEmpty": true,
	"mail.err.consentSourceRequired": true,
	"mail.err.importEmpty": true,
	"mail.err.importTooLarge": true,
	"mail.err.campaignNotDraft": true,
	"mail.err.campaignNoRecipient": true,
	"mail.err.cipherSecretMissing": true,
	"mail.err.cipherUnavailable": true,
	"mail.err.templateSyntax": true,
	"mail.err.campaignSending": true,
	"mail.err.automationNotFound": true,
	"mail.err.automationTriggerInvalid": true,
	"mail.err.automationGraphInvalid": true,
	"mail.err.automationRunExists": true,
	"mail.err.automationRunNotFound": true,
	"mail.err.trackLinkInvalid": true,
	"mail.err.unsubscribeLinkInvalid": true,
	"mail.msg.unsubscribeDoneTitle": true,
	"mail.msg.unsubscribeDoneBody": true,
	"mail.msg.unsubscribeDoneNote": true,
	"mail.err.internal": true,
	"mail.bulk.allDone": true,
	"mail.bulk.allSkipped": true,
	"mail.bulk.partial": true,
	"mail.msg.automationTick": true,
	"mail.page.templateListFailed": true,
	"mail.page.contactStatusBad": true,
	"mail.page.campaignIDRequired": true,
	"mail.page.runIDRequired": true,
	"mail.test.mailSubject": true,
	"mail.test.mailText": true,
	"mail.test.mailHtml": true,
}

const mailEnumMinMessageConsts = 40

func isMessageConst(name string) bool {
	for _, prefix := range []string{"Err", "Msg", "Notice"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return strings.HasPrefix(name, "TestMail") && !strings.HasSuffix(name, "Fallback")
}

func TestMailEnumKeysAreWellFormed(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}

	seen := map[string]string{}
	legacySeen := map[string]bool{}
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
					if strings.Contains(value, ".") {
						if !mailLegacyDottedKeys[value] {
							t.Errorf("%s 的 %s = %q 是新增的点分 key：新词条必须是 1–15 位 ASCII 字母数字、首字母，"+
								"存量点分 key 只能从台账里减少", file, name.Name, value)
							continue
						}
						legacySeen[value] = true
					} else if !mailFlatKeyShape.MatchString(value) {
						t.Errorf("%s 的 %s = %q 不是合法短 key：首字符必须为字母、仅 ASCII 字母数字、长度 1–15；"+
							"值就是 sys_i18n 的主键，形状不对时取词永远命中不了",
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

	for key := range mailLegacyDottedKeys {
		if !legacySeen[key] {
			t.Errorf("存量台账里的 %q 在常量中已不存在：改成短 key 后请同步删掉台账条目", key)
		}
	}

	if checked < mailEnumMinMessageConsts {
		t.Fatalf("只解析到 %d 个文案常量（阈值 %d）：文件路径或解析逻辑写错了，测试会变成空转",
			checked, mailEnumMinMessageConsts)
	}
}
