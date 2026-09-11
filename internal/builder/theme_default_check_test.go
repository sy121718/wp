package builder

import (
	"encoding/json"
	"testing"
)

// TestDefaultThemeSettingsValid 默认主题必须能过自身校验、能序列化。
func TestDefaultThemeSettingsValid(t *testing.T) {
	theme := DefaultThemeSettings()
	if err := ValidateThemeSettings(theme); err != nil {
		t.Fatalf("默认主题未通过校验: %v", err)
	}
	raw, err := json.Marshal(theme)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	back, err := ParseThemeSettings(raw)
	if err != nil {
		t.Fatalf("回解析失败: %v", err)
	}
	if back.Colors.Primary != theme.Colors.Primary {
		t.Errorf("往返后主色不一致: %q", back.Colors.Primary)
	}
}
