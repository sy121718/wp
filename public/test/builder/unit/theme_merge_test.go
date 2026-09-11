package unit

// theme_merge_test.go — 三层继承的中间层语义（主题 → 页面覆盖 → 组件）。
//
// 关键约定：每一层的「空」都表示继承上一层。页面只改主色时其余令牌必须继续跟随主题；
// 主题日后改了字体，页面没覆盖过的字体项要跟着变、覆盖过的项保持不变。

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// TestMergeThemeSettingsKeepsUncoveredKeys 只覆盖主色，其余令牌继续跟随主题。
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

// TestMergeThemeRawJSONTreatsEmptyAsUnset 空串/null/空对象都算「未覆盖」。
// 面板把「留空」提交成 ""，不能拿它把主题的值盖掉，否则一存页面主题就全空。
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

// TestMergeThemeRawJSONPreservesUnknownKeys 保真：结构体不认识的键不能在合并中丢失。
// （快照是展示层数据，历史/未来版本的键都要原样带过去。）
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

// TestMergeThemeRawJSONEmptyBothSides 两边都空 → 返回空（调用方写 `{}` 快照）。
func TestMergeThemeRawJSONEmptyBothSides(t *testing.T) {
	if got := builder.MergeThemeRawJSON(nil, nil); len(got) != 0 {
		t.Errorf("两边都空应返回空，got %s", string(got))
	}
	// 只有主题、没有覆盖：原样返回主题。
	theme := json.RawMessage(`{"colors":{"primary":"#111111"}}`)
	if got := builder.MergeThemeRawJSON(theme, nil); string(got) != string(theme) {
		t.Errorf("无覆盖时应原样返回主题，got %s", string(got))
	}
}
