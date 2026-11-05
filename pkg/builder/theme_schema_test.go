package builder

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"
)

func TestInjectThemeIDPreservesKeysAndOverwrites(t *testing.T) {
	base, err := json.Marshal(map[string]any{
		"colors":  map[string]any{"primary": "#000"},
		"themeId": "old",
	})
	if err != nil {
		t.Fatalf("构造输入失败: %v", err)
	}
	got := InjectThemeID(base, "11111111-2222-3333-4444-555555555555")
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("注入结果非法 JSON: %v", err)
	}
	if m["themeId"] != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("themeId 未被覆盖: %v", m["themeId"])
	}
	colors, ok := m["colors"].(map[string]any)
	if !ok || colors["primary"] != "#000" {
		t.Fatalf("保真失败，colors 被破坏: %v", m["colors"])
	}
}

func TestInjectThemeIDToleratesBadInput(t *testing.T) {
	if got := InjectThemeID(json.RawMessage("{bad"), "x"); string(got) != "{bad" {
		t.Fatalf("非法 JSON 应原样返回, got %s", got)
	}
	base := json.RawMessage(`{"a":1}`)
	if got := InjectThemeID(base, ""); string(got) != string(base) {
		t.Fatalf("空标识应原样返回, got %s", got)
	}
}

func TestThemeVarsCSSEmitsThemeID(t *testing.T) {
	withID := &ThemeSettings{ThemeID: "abc-123", Colors: ThemeColors{Primary: "#000"}}
	css := ThemeVarsCSS(withID)
	if !strings.Contains(css, "--sky-theme-id") {
		t.Fatalf("缺少 --sky-theme-id 变量: %s", css)
	}
	noID := &ThemeSettings{Colors: ThemeColors{Primary: "#000"}}
	if strings.Contains(ThemeVarsCSS(noID), "sky-theme-id") {
		t.Fatalf("无标识时不应输出变量: %s", ThemeVarsCSS(noID))
	}

	bad := &ThemeSettings{ThemeID: "abc$evil", Colors: ThemeColors{Primary: "#000"}}
	if strings.Contains(ThemeVarsCSS(bad), "evil") {
		t.Fatalf("非法标识应被白名单拒绝: %s", ThemeVarsCSS(bad))
	}
}

func TestEmptyStructureBindingsMarshalClearsSlots(t *testing.T) {
	raw, err := json.Marshal(StructureBindings{})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(raw) != "{}" {
		t.Fatalf("空绑定应序列化为 {}, got %s", raw)
	}
}
