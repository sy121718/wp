package builder

import (
	"encoding/json"
	"strings"
	"testing"
)

// VIS-002：themeId 是只读元数据，注入必须保真（不丢历史键）且可覆盖旧值。
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

// VIS-002：非法 JSON 与空标识按原样返回（保真优先，不捏造快照）。
func TestInjectThemeIDToleratesBadInput(t *testing.T) {
	if got := InjectThemeID(json.RawMessage("{bad"), "x"); string(got) != "{bad" {
		t.Fatalf("非法 JSON 应原样返回, got %s", got)
	}
	base := json.RawMessage(`{"a":1}`)
	if got := InjectThemeID(base, ""); string(got) != string(base) {
		t.Fatalf("空标识应原样返回, got %s", got)
	}
}

// VIS-008：主题标识随 :root 变量进产物（导航栏据此与当前主题一致）；
// 无标识时零输出，既有产物字节不变。
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
	// 注入防御：标识含非白名单字符（$）时整段丢弃。
	bad := &ThemeSettings{ThemeID: "abc$evil", Colors: ThemeColors{Primary: "#000"}}
	if strings.Contains(ThemeVarsCSS(bad), "evil") {
		t.Fatalf("非法标识应被白名单拒绝: %s", ThemeVarsCSS(bad))
	}
}

// VIS-008：换主题时清空槽位绑定落库的字节依据 —— 空绑定序列化为 {}，
// jsonb_set 覆盖后页面 settings.structure 不残留旧主题排版。
func TestEmptyStructureBindingsMarshalClearsSlots(t *testing.T) {
	raw, err := json.Marshal(StructureBindings{})
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(raw) != "{}" {
		t.Fatalf("空绑定应序列化为 {}, got %s", raw)
	}
}
