package routers

// write_endpoint_contract_test.go — 后台模板的 JSON 写端点与路由表对账。
//
// 为什么需要它：`data-write-api` 是**字符串**，模板侧写错一个字母不会有任何编译错，
// 运行时症状是「按钮点下去没反应」（fetch 404，基座把失败写进结论行，而结论行一开始
// 是空的 —— 看起来像点了没生效）。本仓已经发生过两次真实故障：
//
//   - C 批把 endpoint 注册在根组（`/api/translations/save` 而不是 `/api/product/translations/save`），
//     只有重写 routes.snapshot 才暴露；
//   - E 批删掉 6 条页面写路由，若模板里还留着引用，前端就 404 —— 判断「模板有没有引用」
//     当时靠 rg 人肉看。
//
// 判据三条（都是「不报错但行为错」的形状，所以必须机器守）：
//
//  1. 模板声明的写端点必须在 testdata/routes.snapshot 里且方法为 POST；
//  2. `data-write-mode` 取值必须是基座真的实现了的那四档 —— 写错档位时基座**静默降级**
//     成 single（只发 id，其余字段全丢），端点收到一个「看着有 id 但什么都没改」的身体；
//  3. 写按钮必须带 `data-requires-js`：无 JS 时模板里这些按钮是 hidden + disabled，
//     靠基座起来后摘掉；漏写这一个属性 = 无 JS 环境下按钮可见可点，点下去什么都不会
//     发生。**判据按 DOM 归属判**，不是只看按钮自己 ——
//     · 按钮自身带 `data-write-mode`（submitter 级声明），或
//     · 按钮所属的 <form> 带 `data-write-api`（表单级声明：商品批量调价、单行删除、
//     订单流转、退货审批、优惠券启停都是这一形态），或
//     · 按钮的**任一祖先**带 `data-requires-js`（navigation 的 ↑↓ 用 <span> 包住按钮）。
//     只扫按钮自身时，表单级声明下的提交按钮一个都扫不到 —— 而那正是本仓的主流形态。
//
// 只读文件，不需要数据库（不装配路由）：快照就是路由表的权威复本，模板改了没重写
// 快照的漂移由 TestRouteSnapshotStable 负责抓。

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

// writeModesSupported 基座 ui/json_write.js 真的实现了的档位（见该文件头部的契约）。
var writeModesSupported = map[string]bool{"single": true, "bulk": true, "create": true, "form": true}

// loadRouteSnapshot 读路由快照成 "METHOD PATH" 集合。
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

// loadAdminTemplates 读全部后台模板（相对路径 → 内容）。
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

// expandWriteEndpoints 把一个 data-write-api 值展开成具体路径。
//
// 模板里有两处用 Jet 条件在 create / update 之间切换（标签、分类、属性组表单），
// 展开成两个路径分别对账。出现别的动态形态（纯变量、循环变量）时判失败：
// 那种写法的端点无法对账，等于把这条闸门关掉了。
func expandWriteEndpoints(rel, value string) []string {
	if m := jetCondWrite.FindStringSubmatch(value); m != nil {
		return []string{m[1] + m[2] + m[4], m[1] + m[3] + m[4]}
	}
	if strings.Contains(value, "{{") || !strings.HasPrefix(value, "/") {
		return nil
	}
	return []string{value}
}

// TestWriteAPIEndpointsExistInRouteTable 模板声明的每个写端点都必须在路由表里。
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

// TestWriteModeValuesAreSupported data-write-mode 只能取基座实现了的四档。
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

// TestWriteButtonsRequireJS 写按钮（自身或所属表单声明了写端点）必须带 data-requires-js。
//
// 无 JS 时这些按钮在模板里是 hidden + disabled，由基座起来后摘掉；漏写这个属性，
// 无 JS 环境下按钮可见可点，点下去什么都不会发生（表单提交被 data-write-api 的 JS 交接
// 假定接管，而 JS 根本不在）。
//
// 判据按 DOM 归属，见文件头第 3 条。两个刻意的豁免：
//
//	· 表单另带 method="post" + action（原生提交有兜底，如商品预览的 target=_blank）；
//	· 按钮带**静态** disabled（无 JS 时按不动，浏览器也不会拿它做隐式提交）。
//	  {{if …}}disabled{{end}} 这种条件态不算静态 —— 条件不成立时它完全可用。
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
			if !found || !strings.Contains(owner, "data-write-api") {
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

// htmlFrame 标签栈的一层：只记判据需要的两件事（是否 form / 是否带 data-requires-js）。
type htmlFrame struct {
	name       string
	attrs      string
	requiresJS bool
}

// htmlToken 一个起始 / 结束标签。
type htmlToken struct {
	closing bool
	name    string
	attrs   string
}

// tagTokenRe 逐标签扫描。属性值里允许出现 > （title="a > b"），所以引号内单独处理。
var tagTokenRe = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9-]*)((?:[^>"']|"[^"]*"|'[^']*')*)>`)

// jetCommentRe Jet 注释 —— 注释里写的示例标签不该参与扫描（本仓注释大量引用 <form>/<button>）。
var jetCommentRe = regexp.MustCompile(`\{\*[\s\S]*?\*\}`)

// voidTags 没有结束标签的元素：不入栈（入栈会让后续 pop 错位）。
var voidTags = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true, "param": true,
	"source": true, "track": true, "wbr": true}

// scanTags 把模板正文扫成标签序列（先剥 Jet 注释）。
func scanTags(body string) []htmlToken {
	body = jetCommentRe.ReplaceAllString(body, "")
	var out []htmlToken
	for _, m := range tagTokenRe.FindAllStringSubmatch(body, -1) {
		out = append(out, htmlToken{closing: m[1] == "/", name: strings.ToLower(m[2]), attrs: m[3]})
	}
	return out
}

// attrValue 取属性值（只认双引号写法 —— 模板里的属性值全部带引号）。
func attrValue(attrs, name string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `="([^"]*)"`)
	if m := re.FindStringSubmatch(attrs); m != nil {
		return m[1]
	}
	return ""
}

// popFrame 关标签时弹栈。按名字回退找最近同名层：HTML 允许隐式闭合（<li>/<p> 不写结束标签），
// 只弹栈顶会在遇到这种写法时把祖先层一起吃掉，之后的归属判定全错。
func popFrame(stack []htmlFrame, name string) []htmlFrame {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].name == name {
			return stack[:i]
		}
	}
	return stack
}

// anyFrameRequiresJS 祖先里有没有 data-requires-js（navigation 的 ↑↓ 用 <span> 包按钮）。
func anyFrameRequiresJS(stack []htmlFrame) bool {
	for _, f := range stack {
		if f.requiresJS {
			return true
		}
	}
	return false
}

// owningForm 按钮归属的 form：form="<id>" 优先（值可以是模板变量前缀，按前缀匹配），
// 否则取最近的祖先 <form>。
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

// hasNativeFallback 表单自带原生提交兜底（method=post + action），不需要 JS 交接。
func hasNativeFallback(formAttrs string) bool {
	return strings.Contains(formAttrs, "method=\"post\"") || strings.Contains(formAttrs, "action=\"")
}

// hasStaticDisabled 按钮带**静态** disabled（不含模板条件）—— 无 JS 时按不动。
func hasStaticDisabled(buttonAttrs string) bool {
	if !regexp.MustCompile(`\bdisabled\b`).MatchString(buttonAttrs) {
		return false
	}
	return !strings.Contains(buttonAttrs, "{{if")
}

// isVoidTag 见 voidTags。
func isVoidTag(name string) bool { return voidTags[name] }
