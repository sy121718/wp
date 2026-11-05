package core

import (
	"strings"
	"testing"

	json "go_wp/pkg/jsonx"
)

type legacyProbe struct {
	Label string `json:"label,omitempty"`
}

var probeLegacy = []LegacyKey{{From: "buttonText", To: "label"}}

func (p *legacyProbe) NormalizeLegacy(raw json.RawMessage) error {
	return ApplyLegacyKeysInto(raw, p, probeLegacy)
}

func TestLegacyKeyMovesValueToNewField(t *testing.T) {
	got, err := DecodeProps[legacyProbe](json.RawMessage(`{"buttonText":"了解更多"}`))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.Label != "了解更多" {
		t.Fatalf("旧键 buttonText 的值没落到 Label 上：%q —— 存量文档的按钮会整颗消失", got.Label)
	}
}

func TestLegacyKeyPrefersNewValue(t *testing.T) {
	got, err := DecodeProps[legacyProbe](json.RawMessage(`{"label":"新文案","buttonText":"旧文案"}`))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.Label != "新文案" {
		t.Errorf("应以新键为准，得到 %q —— 反过来会让作者的修改被历史残留覆盖", got.Label)
	}
}

func TestLegacyKeyTreatsEmptyNewValueAsMissing(t *testing.T) {
	got, err := DecodeProps[legacyProbe](json.RawMessage(`{"label":"","buttonText":"旧文案"}`))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.Label != "旧文案" {
		t.Errorf("新键为空串时应回落到旧值，得到 %q", got.Label)
	}
}

func TestLegacyKeyNoOpOnCleanDocument(t *testing.T) {
	got, err := DecodeProps[legacyProbe](json.RawMessage(`{"label":"新文案"}`))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.Label != "新文案" {
		t.Errorf("干净文档被改动了：%q", got.Label)
	}

	if _, err := DecodeProps[legacyProbe](nil); err != nil {
		t.Errorf("空 props 不该报错: %v", err)
	}
}

func TestApplyLegacyKeysDropsOldKey(t *testing.T) {
	out, err := ApplyLegacyKeys(json.RawMessage(`{"buttonText":"X","title":"T"}`), probeLegacy)
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "buttonText") {
		t.Errorf("归一后的 raw 里仍有旧键：%s", s)
	}
	if !strings.Contains(s, `"label":"X"`) {
		t.Errorf("归一后的 raw 里没有新键：%s", s)
	}
	if !strings.Contains(s, `"title":"T"`) {
		t.Errorf("归一丢了无关字段：%s", s)
	}
}
