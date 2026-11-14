package projecthttp

import (
	"reflect"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
)

// themeSettingsFormTags 返回主题设置 DTO 的全部 form tag。
func themeSettingsFormTags(t *testing.T) map[string]bool {
	t.Helper()
	typ := reflect.TypeOf(projectdto.ThemeSettingsFormReq{})
	tags := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("form"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		tags[tag] = true
	}
	return tags
}

// themeSettingsOldPostFormKeys 是改造前处理器逐字读取的 44 个设计令牌键。
// 它们必须逐字出现在 DTO 的 form tag 里，否则该令牌会静默回落到内置默认。
var themeSettingsOldPostFormKeys = []string{
	"images.lazyLoad", "images.skeleton",
	"colors.primary", "colors.secondary", "colors.accent", "colors.success",
	"colors.warning", "colors.danger", "colors.text", "colors.heading",
	"colors.background", "colors.surface", "colors.border",
	"typography.heading.color", "typography.heading.weight", "typography.heading.size",
	"typography.heading.spacing", "typography.heading.font",
	"typography.body.color", "typography.body.size", "typography.body.line", "typography.body.font",
	"typography.link.color", "typography.link.hover", "typography.link.underline",
	"button.background", "button.color", "button.radius", "button.weight",
	"button.py", "button.px", "button.hoverBg", "button.hoverColor",
	"button.borderWidth", "button.borderStyle", "button.borderColor", "button.shadow",
	"surface.radius", "surface.borderWidth", "surface.borderColor", "surface.shadow",
	"motion.duration", "motion.easing", "motion.entrance",
}

func TestThemeSettingsDTOKeepsOriginalKeys(t *testing.T) {
	if len(themeSettingsOldPostFormKeys) != 44 {
		t.Fatalf("改造前的键表应恰好 44 个，实际 %d", len(themeSettingsOldPostFormKeys))
	}
	tags := themeSettingsFormTags(t)
	for _, key := range themeSettingsOldPostFormKeys {
		if !tags[key] {
			t.Errorf("原手工读取键 %q 没有出现在 ThemeSettingsFormReq 的 form tag 里", key)
		}
	}
}

// TestThemeSettingsFieldGroupNamesInDTO 证明模板循环渲染的 name 集合是静态可判定的：
// themeFieldGroups 是编译期固定的 Go 表，其中的每个 Name 都必须在 DTO 的 form tag 里。
// 模板用 {{f.Name}} 展开，对模板扫描器仍是动态名；本测试在 Go 侧把这张表钉死。
func TestThemeSettingsFieldGroupNamesInDTO(t *testing.T) {
	tags := themeSettingsFormTags(t)
	for _, group := range themeFieldGroups {
		for _, field := range group.Fields {
			if !tags[field.Name] {
				t.Errorf("字段表键 %q（组 %q）不在 ThemeSettingsFormReq 的 form tag 里", field.Name, group.Title)
			}
		}
	}
}
