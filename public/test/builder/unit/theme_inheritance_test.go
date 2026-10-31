package unit

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
)

var themeColorAllow = map[string]string{
	"core.social_buttons": "各平台品牌色（facebook/x/instagram…）是视觉标识，必须固定，不能跟主题走",
}

var themeColorValueAllow = []string{

	"rgba(12,14,26,.72)",
}

var colorDeclRe = regexp.MustCompile(`(?i)^(color|background|background-color|border|border-[a-z]+|border-color|fill|stroke|outline-color)\s*:\s*(.+)$`)

var nonNeutralColorRe = regexp.MustCompile(`(?i)(#[0-9a-f]{3,8}\b|rgba?\(|hsla?\()`)

var neutralColorRe = regexp.MustCompile(`(?i)^(?:#(?:fff|ffffff|000|000000)\b|rgba?\(\s*(?:0\s*,\s*0\s*,\s*0|255\s*,\s*255\s*,\s*255)\s*[,)]|hsla?\(\s*0\s*,\s*0%)`)

func isNeutralColor(val string) bool { return neutralColorRe.MatchString(strings.TrimSpace(val)) }

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
