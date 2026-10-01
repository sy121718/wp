package templates

// admin_nav_responsive_test.go — 后台全局布局的窄屏断点契约（审计条目 UI-001）。
//
// 修复前：.rail（88px 图标栏）+ .subnav（224px 二级栏）在 .admin-layout 里固定并排，
// 且这三个选择器没有任何 @media —— 375px 视口下导航吃掉 312px，内容区只剩几十像素。
//
// 本测试钉住三件事，防止后续改动把断点改回去或漏掉桌面端：
//  1. theme.css 里 rail / subnav 的脱离文档流规则只允许出现在 1023px 媒体查询内，
//     媒体查询外的基础规则仍是原来的 flex 并排（桌面端零改动）；
//  2. 汉堡按钮与遮罩在媒体查询外默认 display: none（桌面端不出现、不占位）；
//  3. 后台外壳渲染后带 navToggle / data-nav-scrim，且 admin.js 可解析（node --check）。

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// stripCSSComments 返回与 src 等长的副本，把注释内容替换成空格（保留换行）。
// 下标与原文一一对应，供后续用同一偏移在原文上切片。
//
// 必须有这一步：注释里出现字面量 "@media"（例如「这三个选择器没有任何 @media 规则」
// 这类说明）会让朴素的子串扫描把注释当成媒体查询开头，接下来的花括号配平一路错位，
// 媒体查询外的基础规则被误判成块内 —— 本文件第一次跑就是这样误报的。
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

// splitCSSMedia 把 CSS 切成「媒体查询外的基础区」与「各媒体查询块」。
// 在去注释的掩码上定位、按花括号配平切分（theme.css 的规则体里没有字面量花括号）。
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

// matchBrace 返回与 src[open]（花括号）配对的收尾花括号下标；不配平时返回末字符下标。
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

// blockWith 返回以给定前缀开头的媒体查询块；没有则返回空串。
func blockWith(blocks []string, prefix string) string {
	for _, b := range blocks {
		if strings.HasPrefix(strings.TrimSpace(b), prefix) {
			return b
		}
	}
	return ""
}

// ruleBody 返回选择器规则体（选择器后第一对花括号之间的内容）；找不到返回空串。
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
	css, err := os.ReadFile("static/css/theme.css")
	if err != nil {
		t.Fatalf("读取 theme.css 失败: %v", err)
	}
	return string(css)
}

// TestAdminLayoutDesktopRulesStayOutsideMediaQuery 桌面端布局必须仍在媒体查询之外：
// rail / subnav 的 fixed 与 transform 只许出现在断点块里，基础区保持原 flex 并排。
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

	// 桌面基础布局必须原样保留：flex 并排 + 两栏固定宽度。
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

// TestAdminNavBreakpointRules 断点块必须把 rail / subnav 变成覆盖式抽屉，
// 且关闭态位移要把「图标栏 + 二级栏」整体移出视口（只写 -100% 会在左侧露出 88px）。
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

// TestAdminNavBreakpointTakesOverCollapsedWidth 断点块必须把 sidebar-collapsed 的占位宽度
// 也接管回来（实测故障：cookie sidebar_open=0 的用户把窗口拉窄后点开抽屉，二级栏只剩
// 21px 宽 —— padding 20px + 边框 1px，内容整块看不见）。
//
// 为什么必须显式接管：基础区 `body.sidebar-collapsed .subnav { width: 0 }` 特异性 (0,2,1)，
// 压得过断点块里低特异性的 `.subnav { width: min(...) }`；断点块原先只接管了
// flex-basis / opacity / border-right，宽度就被桌面收起态赢走了。
//
// 断言取「含 body.sidebar-collapsed .subnav 的那条规则体」并要求其中自带 width，
// 而不是在整块里做子串匹配 —— 整块匹配会被同块里别的规则或注释满足，退化成假绿。
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

	// 两处表达式必须逐字一致：各写一套值迟早漂移，抽屉宽度与桌面占位宽度就分成两个真源。
	if subnavBody := ruleBody(mobile, ".subnav {"); !strings.Contains(subnavBody, "width: "+want) {
		t.Errorf("断点块里 .subnav 的宽度表达式与接管条不一致（应同为 \"width: %s\"）", want)
	}
}

// TestNavToggleHiddenOnDesktop 汉堡按钮与遮罩在桌面端必须默认不渲染不占位。
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

// TestAdminLayoutRendersNavToggle 真渲染后台外壳：汉堡按钮与遮罩必须真的在 HTML 里，
// 且按钮带无障碍属性（aria-expanded / aria-controls 指向图标栏与二级栏）。
func TestAdminLayoutRendersNavToggle(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
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
	// 遮罩不加 hidden 属性：显隐完全交给媒体查询，避免属性与 display 打架。
	if strings.Contains(html, `data-nav-scrim hidden`) {
		t.Error("遮罩不应带 hidden 属性（与断点里的 display 冲突）")
	}
}

// TestAdminNavDrawerScriptParses admin.js 必须能被 Node 当普通脚本解析，
// 且窄屏断点值与 CSS 一致（1023px 两头写错会表现成「点了没反应」）。
func TestAdminNavDrawerScriptParses(t *testing.T) {
	src, err := os.ReadFile("static/js/admin.js")
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
