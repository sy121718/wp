package routers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	writeAPIRe   = regexp.MustCompile(`data-write-(?:api|url)="([^"]+)"`)
	writeModeRe  = regexp.MustCompile(`data-write-mode="([^"]+)"`)
	buttonTagRe  = regexp.MustCompile(`(?s)<button\b[^>]*>`)
	jetCondWrite = regexp.MustCompile(`^(.*?)\{\{if[^}]*\}\}([^{}]*)\{\{else\}\}([^{}]*)\{\{end\}\}(.*)$`)
)

var writeModesSupported = map[string]bool{"single": true, "bulk": true, "create": true, "form": true}

func loadRouteSnapshot(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "routes.snapshot"))
	if err != nil {
		t.Fatalf("读取路由快照失败: %v", err)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			set[line] = true
		}
	}
	if len(set) == 0 {
		t.Fatal("路由快照为空")
	}
	return set
}

func loadAdminTemplates(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "templates", "admin")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".html") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("扫描后台模板失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("后台模板目录为空：%s", root)
	}
	return files
}

func expandWriteEndpoints(rel, value string) []string {
	if m := jetCondWrite.FindStringSubmatch(value); m != nil {
		return []string{m[1] + m[2] + m[4], m[1] + m[3] + m[4]}
	}
	if strings.Contains(value, "{{") || !strings.HasPrefix(value, "/") {
		return nil
	}
	return []string{value}
}

func TestWriteAPIEndpointsExistInRouteTable(t *testing.T) {
	routes := loadRouteSnapshot(t)
	var missing, dynamic, total int
	for rel, body := range loadAdminTemplates(t) {
		for _, m := range writeAPIRe.FindAllStringSubmatch(body, -1) {
			eps := expandWriteEndpoints(rel, m[1])
			if len(eps) == 0 {
				dynamic++
				t.Errorf("%s: data-write-api=%q 含无法对账的动态值", rel, m[1])
				continue
			}
			for _, ep := range eps {
				total++
				if !routes["POST "+ep] {
					missing++
					t.Errorf("%s: 写端点 %s 不在路由表（基座 fetch 会 404，按钮点了没反应）", rel, ep)
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("没有扫到任何 data-write-api：模板约定或本测试的匹配串已经漂移")
	}
	t.Logf("写端点对账：%d 个（动态 %d 个），缺失 %d 个", total, dynamic, missing)
}

func TestWriteModeValuesAreSupported(t *testing.T) {
	for rel, body := range loadAdminTemplates(t) {
		for _, m := range writeModeRe.FindAllStringSubmatch(body, -1) {
			if !writeModesSupported[m[1]] {
				t.Errorf("%s: data-write-mode=%q 不是基座支持的档位（single/bulk/create/form）——"+
					"写错时基座静默降级成 single，只发 id，其余字段全丢", rel, m[1])
			}
		}
	}
}

func TestWriteButtonsRequireJS(t *testing.T) {
	var checked, exemptNative, exemptDisabled int
	for rel, body := range loadAdminTemplates(t) {
		formAttrs := map[string]string{}
		for _, tok := range scanTags(body) {
			if tok.closing || tok.name != "form" {
				continue
			}
			if id := attrValue(tok.attrs, "id"); id != "" && !strings.Contains(id, "{{") {
				formAttrs[id] = tok.attrs
			}
		}

		stack := []htmlFrame{}
		for _, tok := range scanTags(body) {
			if tok.closing {
				stack = popFrame(stack, tok.name)
				continue
			}
			if tok.name != "button" || !strings.Contains(tok.attrs, "type=\"submit\"") {
				if !isVoidTag(tok.name) {
					stack = append(stack, htmlFrame{name: tok.name, attrs: tok.attrs, requiresJS: strings.Contains(tok.attrs, "data-requires-js")})
				}
				continue
			}

			owner, found := owningForm(tok.attrs, stack, formAttrs)

			if !found || !strings.Contains(owner, "data-write-url") {
				continue
			}
			checked++
			if hasNativeFallback(owner) {
				exemptNative++
				continue
			}
			if hasStaticDisabled(tok.attrs) {
				exemptDisabled++
				continue
			}
			if strings.Contains(tok.attrs, "data-requires-js") || anyFrameRequiresJS(stack) {
				continue
			}
			flat := strings.Join(strings.Fields(tok.attrs), " ")
			t.Errorf("%s: 写表单的提交按钮缺 data-requires-js：%s", rel, flat)
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到写表单的提交按钮：模板约定或本测试的匹配串已经漂移")
	}
	t.Logf("写按钮防呆：%d 个（原生兜底 %d 个、静态 disabled %d 个豁免）", checked, exemptNative, exemptDisabled)
}

type htmlFrame struct {
	name       string
	attrs      string
	requiresJS bool
}

type htmlToken struct {
	closing bool
	name    string
	attrs   string
}

var tagTokenRe = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9-]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)

var jetCommentRe = regexp.MustCompile(`\{\*[\s\S]*?\*\}`)

var voidTags = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true, "param": true,
	"source": true, "track": true, "wbr": true}

func scanTags(body string) []htmlToken {
	body = jetCommentRe.ReplaceAllString(body, "")
	var out []htmlToken
	for _, m := range tagTokenRe.FindAllStringSubmatch(body, -1) {
		out = append(out, htmlToken{closing: m[1] == "/", name: strings.ToLower(m[2]), attrs: m[3]})
	}
	return out
}

func attrValue(attrs, name string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `="([^"]*)"`)
	if m := re.FindStringSubmatch(attrs); m != nil {
		return m[1]
	}
	return ""
}

func popFrame(stack []htmlFrame, name string) []htmlFrame {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].name == name {
			return stack[:i]
		}
	}
	return stack
}

func anyFrameRequiresJS(stack []htmlFrame) bool {
	for _, f := range stack {
		if f.requiresJS {
			return true
		}
	}
	return false
}

func owningForm(buttonAttrs string, stack []htmlFrame, forms map[string]string) (string, bool) {
	if raw := attrValue(buttonAttrs, "form"); raw != "" {
		if strings.Contains(raw, "{{") {
			prefix := raw[:strings.Index(raw, "{{")]
			for id, attrs := range forms {
				if prefix != "" && strings.HasPrefix(id, prefix) {
					return attrs, true
				}
			}
			return "", false
		}
		attrs, ok := forms[raw]
		return attrs, ok
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].name == "form" {
			return stack[i].attrs, true
		}
	}
	return "", false
}

func hasNativeFallback(formAttrs string) bool {
	return strings.Contains(formAttrs, "method=\"post\"") || strings.Contains(formAttrs, "action=\"")
}

func hasStaticDisabled(buttonAttrs string) bool {
	if !regexp.MustCompile(`\bdisabled\b`).MatchString(buttonAttrs) {
		return false
	}
	return !strings.Contains(buttonAttrs, "{{if")
}

func isVoidTag(name string) bool { return voidTags[name] }
