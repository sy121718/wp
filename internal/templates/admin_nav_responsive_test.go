package templates

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

func stripCSSComments(src string) string {
	out := []byte(src)
	for i := 0; i+1 < len(out); {
		if out[i] == '/' && out[i+1] == '*' {
			j := i + 2
			for j+1 < len(out) && !(out[j] == '*' && out[j+1] == '/') {
				j++
			}
			end := j + 2
			if end > len(out) {
				end = len(out)
			}
			for k := i; k < end; k++ {
				if out[k] != '\n' {
					out[k] = ' '
				}
			}
			i = end
			continue
		}
		i++
	}
	return string(out)
}

func splitCSSMedia(src string) (string, []string) {
	mask := stripCSSComments(src)
	var base strings.Builder
	var blocks []string
	last := 0
	for {
		i := strings.Index(mask[last:], "@media")
		if i < 0 {
			break
		}
		i += last
		base.WriteString(src[last:i])
		open := strings.Index(mask[i:], "{")
		if open < 0 {
			break
		}
		open += i
		end := matchBrace(mask, open)
		blocks = append(blocks, src[i:end+1])
		last = end + 1
	}
	base.WriteString(src[last:])
	return base.String(), blocks
}

func matchBrace(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(src) - 1
}

func blockWith(blocks []string, prefix string) string {
	for _, b := range blocks {
		if strings.HasPrefix(strings.TrimSpace(b), prefix) {
			return b
		}
	}
	return ""
}

func ruleBody(src, selector string) string {
	i := strings.Index(src, selector)
	if i < 0 {
		return ""
	}
	open := strings.Index(src[i:], "{")
	if open < 0 {
		return ""
	}
	open += i
	closing := strings.Index(src[open:], "}")
	if closing < 0 {
		return ""
	}
	return src[open+1 : open+closing]
}

func readAdminThemeCSS(t *testing.T) string {
	t.Helper()
	css, err := AssetFile("css/theme.css")
	if err != nil {
		t.Fatalf("读取 theme.css 失败: %v", err)
	}
	return string(css)
}

func TestAdminLayoutDesktopRulesStayOutsideMediaQuery(t *testing.T) {
	base, blocks := splitCSSMedia(readAdminThemeCSS(t))

	for _, sel := range []string{".rail {", ".subnav {"} {
		body := ruleBody(base, sel)
		if body == "" {
			t.Fatalf("媒体查询外找不到基础规则 %s（桌面布局被改没了）", sel)
		}
		for _, banned := range []string{"position: fixed", "transform:"} {
			if strings.Contains(body, banned) {
				t.Errorf("媒体查询外的 %s 里出现了窄屏专用声明 %q（桌面端布局被改）", sel, banned)
			}
		}
	}

	for _, want := range []string{
		".admin-layout {",
		"display: flex;",
		"flex: 0 0 var(--rail-w, 88px); width: var(--rail-w, 88px);",
		"flex: 0 0 var(--subnav-w, 224px); width: var(--subnav-w, 224px);",
	} {
		if !strings.Contains(base, want) {
			t.Errorf("媒体查询外的桌面基础布局缺少 %q（应保持原样）", want)
		}
	}

	if len(blocks) == 0 {
		t.Fatal("theme.css 里没有解析到任何媒体查询块")
	}
}

func TestAdminNavBreakpointRules(t *testing.T) {
	_, blocks := splitCSSMedia(readAdminThemeCSS(t))
	mobile := blockWith(blocks, "@media (max-width: 1023px)")
	if mobile == "" {
		t.Fatal("缺少 @media (max-width: 1023px) 断点块（审计 UI-001 的核心修复）")
	}
	for _, want := range []string{
		"position: fixed",
		"body.nav-open .rail, body.nav-open .subnav { transform: translateX(0); }",
		"transform: translateX(calc(-100% - var(--rail-w, 88px)))",
		"width: min(var(--subnav-w, 224px), calc(100vw - var(--rail-w, 88px)))",
		"body.sidebar-collapsed .subnav",
		"body.nav-open .nav-scrim { display: block; }",
	} {
		if !strings.Contains(mobile, want) {
			t.Errorf("1023px 断点块缺少 %q", want)
		}
	}
}

func TestAdminNavBreakpointTakesOverCollapsedWidth(t *testing.T) {
	_, blocks := splitCSSMedia(readAdminThemeCSS(t))
	mobile := blockWith(blocks, "@media (max-width: 1023px)")
	if mobile == "" {
		t.Fatal("缺少 @media (max-width: 1023px) 断点块")
	}
	const want = "min(var(--subnav-w, 224px), calc(100vw - var(--rail-w, 88px)))"

	body := ruleBody(mobile, "body.sidebar-collapsed .subnav")
	if body == "" {
		t.Fatal("断点块里找不到 body.sidebar-collapsed .subnav 规则（窄屏接管桌面收起态的唯一出口）")
	}
	if !strings.Contains(body, "width: "+want) {
		t.Errorf("断点块里 body.sidebar-collapsed .subnav 没有接管 width（缺少 \"width: %s\"）：\n"+
			"漏掉它时基础区的 width: 0 会赢，窄屏抽屉被压成 padding+border = 21px，二级栏内容不可见。\n"+
			"实际规则体：%s", want, strings.TrimSpace(body))
	}

	if subnavBody := ruleBody(mobile, ".subnav {"); !strings.Contains(subnavBody, "width: "+want) {
		t.Errorf("断点块里 .subnav 的宽度表达式与接管条不一致（应同为 \"width: %s\"）", want)
	}
}

func TestNavToggleHiddenOnDesktop(t *testing.T) {
	base, blocks := splitCSSMedia(readAdminThemeCSS(t))
	if !strings.Contains(base, ".nav-toggle, .nav-scrim { display: none; }") {
		t.Error("媒体查询外缺少 .nav-toggle / .nav-scrim 的 display: none（桌面端会出现汉堡按钮或遮罩）")
	}
	mobile := blockWith(blocks, "@media (max-width: 1023px)")
	if body := ruleBody(mobile, ".nav-toggle {"); !strings.Contains(body, "display: inline-flex") {
		t.Error("1023px 断点块里 .nav-toggle 未改为可见（display: inline-flex），汉堡按钮点不到")
	}
}

func TestAdminLayoutRendersNavToggle(t *testing.T) {
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	data := map[string]any{
		"lang": "zh-CN", "title": "页面管理", "t": TranslateFunc("zh-CN"),
	}
	html, err := render(t, set, "admin/layout", data)
	if err != nil {
		t.Fatalf("后台布局渲染失败: %v", err)
	}
	for _, want := range []string{
		`id="navToggle"`,
		`class="nav-toggle"`,
		`aria-controls="adminRail subnav"`,
		`aria-expanded="false"`,
		`data-label-close="关闭导航"`,
		`class="nav-scrim" data-nav-scrim`,
		`<nav class="rail" id="adminRail">`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("后台外壳缺少 %q", want)
		}
	}

	if strings.Contains(html, `data-nav-scrim hidden`) {
		t.Error("遮罩不应带 hidden 属性（与断点里的 display 冲突）")
	}
}

func TestAdminNavDrawerScriptParses(t *testing.T) {
	src, err := AssetFile("js/admin.js")
	if err != nil {
		t.Fatalf("读取 admin.js 失败: %v", err)
	}
	js := string(src)
	for _, want := range []string{
		"initNavDrawer",
		"matchMedia('(max-width: 1023px)')",
		"nav-open",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("admin.js 缺少 %q", want)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GOWP_REQUIRE_NODE") == "1" {
			t.Fatal("完整控件检查要求 Node")
		}
		t.Skip("缺少 Node")
	}
	cmd := exec.Command(node, "--input-type=commonjs", "--check")
	cmd.Stdin = strings.NewReader(js)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("admin.js 普通脚本解析失败: %v / %s", err, out)
	}
}
