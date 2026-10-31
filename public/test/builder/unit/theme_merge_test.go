package unit

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder"
)

func TestMergeThemeSettingsKeepsUncoveredKeys(t *testing.T) {
	base := &builder.ThemeSettings{}
	base.Colors.Primary = "#111111"
	base.Colors.Border = "#dddddd"
	base.Button.Background = "#222222"

	override := &builder.ThemeSettings{}
	override.Colors.Primary = "#ff0000"

	merged := builder.MergeThemeSettings(base, override)
	if merged.Colors.Primary != "#ff0000" {
		t.Errorf("页面覆盖的主色应生效，got %q", merged.Colors.Primary)
	}
	if merged.Colors.Border != "#dddddd" {
		t.Errorf("页面没覆盖的边框色应跟随主题，got %q", merged.Colors.Border)
	}
	if merged.Button.Background != "#222222" {
		t.Errorf("页面没覆盖的按钮背景应跟随主题，got %q", merged.Button.Background)
	}
}

func TestMergeThemeRawJSONTreatsEmptyAsUnset(t *testing.T) {
	theme := json.RawMessage(`{"colors":{"primary":"#111111","border":"#dddddd"}}`)
	override := json.RawMessage(`{"colors":{"primary":"","border":null}}`)
	merged := builder.MergeThemeRawJSON(theme, override)
	var got map[string]map[string]string
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("合并结果解析失败: %v", err)
	}
	if got["colors"]["primary"] != "#111111" || got["colors"]["border"] != "#dddddd" {
		t.Errorf("空覆盖不应盖掉主题值，got %s", string(merged))
	}
}

func TestMergeThemeRawJSONPreservesUnknownKeys(t *testing.T) {
	theme := json.RawMessage(`{"colors":{"primary":"#111111"},"legacyToken":"keep-me"}`)
	override := json.RawMessage(`{"colors":{"accent":"#00ff00"}}`)
	merged := builder.MergeThemeRawJSON(theme, override)
	if !strings.Contains(string(merged), "keep-me") {
		t.Errorf("未知键在合并中丢失：%s", string(merged))
	}
	if !strings.Contains(string(merged), "#00ff00") {
		t.Errorf("覆盖的键没进去：%s", string(merged))
	}
}

func TestMergeThemeRawJSONEmptyBothSides(t *testing.T) {
	if got := builder.MergeThemeRawJSON(nil, nil); len(got) != 0 {
		t.Errorf("两边都空应返回空，got %s", string(got))
	}

	theme := json.RawMessage(`{"colors":{"primary":"#111111"}}`)
	if got := builder.MergeThemeRawJSON(theme, nil); string(got) != string(theme) {
		t.Errorf("无覆盖时应原样返回主题，got %s", string(got))
	}
}
