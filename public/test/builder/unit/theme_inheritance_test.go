package unit

// theme_inheritance_test.go — 检查组件颜色是否都经主题变量（继承链：主题 → 页面 → 组件）。
//
// 设计约定：组件的「默认」= 继承，最强的在组件自身。
//   主题（全局令牌 → :root 变量）→ 页面（settings.theme 覆盖）→ 组件（props 显式值）
// 组件侧只要写成 var(--sky-*, fallback)，上面两层任何一层给了值都能生效；
// **写死颜色等于把继承链掐断** —— 主题改了那个组件也不动（表单输入框、导航下拉、
// 轮播箭头都曾是这样）。
//
// 规则：颜色属性（color / background* / border*color / border / fill / stroke / outline-color）
// 的声明里出现的颜色字面量必须包在 var(--sky-*) 里（fallback 允许）；
// 纯黑纯白（含带透明度的 rgba）是通用对比色，允许裸用；
// box-shadow / text-shadow 不计（阴影色不参与主题）。
//
// 白名单是「语义固定色」，逐条注明理由 —— 加白名单前先问：这真的是设计固定，还是漏了？

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
)

// themeColorAllow 允许裸用彩色字面量的组件（品牌标识类）。
var themeColorAllow = map[string]string{
	"core.social_buttons": "各平台品牌色（facebook/x/instagram…）是视觉标识，必须固定，不能跟主题走",
}

// themeColorValueAllow 允许的裸色值（语义固定，逐条注明理由）。
var themeColorValueAllow = []string{
	// 卡片放大遮罩：遮罩必须比内容深才有「弹出」感，跟主题走会出现浅色遮罩压不住底。
	"rgba(12,14,26,.72)",
}

// colorDeclRe 匹配「颜色属性: 值」，值里含颜色字面量的声明。
var colorDeclRe = regexp.MustCompile(`(?i)^(color|background|background-color|border|border-[a-z]+|border-color|fill|stroke|outline-color)\s*:\s*(.+)$`)

// nonNeutralColorRe 匹配非纯黑/纯白的颜色字面量（#rgb/#rrggbb/rgb()/hsl()）。
var nonNeutralColorRe = regexp.MustCompile(`(?i)(#[0-9a-f]{3,8}\b|rgba?\(|hsla?\()`)

// neutralColorRe 纯黑/纯白（含带 alpha 的 rgba），属通用对比色，允许裸用。
var neutralColorRe = regexp.MustCompile(`(?i)^(?:#(?:fff|ffffff|000|000000)\b|rgba?\(\s*(?:0\s*,\s*0\s*,\s*0|255\s*,\s*255\s*,\s*255)\s*[,)]|hsla?\(\s*0\s*,\s*0%)`)

// isNeutralColor 该颜色值是否为纯黑/纯白一类通用对比色。
func isNeutralColor(val string) bool { return neutralColorRe.MatchString(strings.TrimSpace(val)) }

// TestComponentColorsFollowTheme 每个组件的颜色声明都必须经主题变量（或属通用对比色）。
func TestComponentColorsFollowTheme(t *testing.T) {
	var violations []string
	audited := 0
	for _, typeName := range core.Types() {
		if reason, ok := themeColorAllow[typeName]; ok {
			t.Logf("白名单跳过 %s：%s", typeName, reason)
			continue
		}
		css, ok := compileComponentCSS(t, typeName)
		if !ok {
			continue
		}
		audited++
		for _, line := range strings.Split(css, "\n") {
			if !colorDeclRe.MatchString(strings.TrimSpace(line)) {
				continue
			}
			if !nonNeutralColorRe.MatchString(line) || strings.Contains(line, "var(--sky-") {
				continue
			}
			allowed := false
			for _, v := range themeColorValueAllow {
				if strings.Contains(line, v) {
					allowed = true
				}
			}
			if allowed {
				continue
			}
			// 抽出颜色值再判是否中性色。
			val := strings.TrimSpace(line[strings.Index(line, ":")+1:])
			if isNeutralColor(val) {
				continue
			}
			violations = append(violations, fmt.Sprintf("%s：%s", typeName, strings.TrimSpace(line)))
		}
	}
	sort.Strings(violations)
	for _, v := range violations {
		t.Errorf("硬编码颜色（应写成 var(--sky-*, fallback) 才能继承主题）：%s", v)
	}
	t.Logf("已审计 %d 个组件的颜色声明", audited)
}

// compileComponentCSS 编译单个组件的默认产物，取它的 CSS。
func compileComponentCSS(t *testing.T, typeName string) (string, bool) {
	t.Helper()
	props := a11yProps[typeName]
	if props == "" {
		props = "{}"
	}
	var children []string
	for i := 0; i < a11yChildCount[typeName]; i++ {
		children = append(children, a11yChild(i))
	}
	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":%q,"props":%s,"children":[%s]}]}`,
		typeName, props, strings.Join(children, ","))
	page, err := builder.ParsePage([]byte(doc))
	if err != nil {
		return "", false
	}
	compiled, err := compile(t, page, builder.WithCollectionResolver(galleryCollection{}))
	if err != nil {
		return "", false
	}
	return compiled.CSS, true
}
