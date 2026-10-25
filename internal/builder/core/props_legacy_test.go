package core

// props_legacy_test.go — 旧 props 名归一的契约（改名不迁移数据）。
//
// 这条契约守的是一个**会静默删内容**的缺陷：props 改名后，存量文档里的旧键
// 反序列化时被忽略 → Label 为空 → BuildView 里「有按钮」的判断为假 → 整颗 <a>
// 不渲染。不报错、不 500，产物里只是少一颗按钮，人工看也未必发现。
//
// 本仓实测影响面：blueprints 2 张、content_templates 1 张、document_snapshots
// 5.78 万条含旧键；97 个已发布实例走商品详情页模板。

import (
	"strings"
	"testing"

	json "go_wp/pkg/jsonx"
)

// legacyProbe 一个带旧名归一的示例 Props（真实组件见 components/*）。
type legacyProbe struct {
	Label string `json:"label,omitempty"`
}

var probeLegacy = []LegacyKey{{From: "buttonText", To: "label"}}

func (p *legacyProbe) NormalizeLegacy(raw json.RawMessage) error {
	return ApplyLegacyKeysInto(raw, p, probeLegacy)
}

// TestLegacyKeyMovesValueToNewField 旧键的值必须落到新字段上。
func TestLegacyKeyMovesValueToNewField(t *testing.T) {
	got, err := DecodeProps[legacyProbe](json.RawMessage(`{"buttonText":"了解更多"}`))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.Label != "了解更多" {
		t.Fatalf("旧键 buttonText 的值没落到 Label 上：%q —— 存量文档的按钮会整颗消失", got.Label)
	}
}

// TestLegacyKeyPrefersNewValue 新键有非空值时，旧键忽略（作者已用新编辑器改过）。
func TestLegacyKeyPrefersNewValue(t *testing.T) {
	got, err := DecodeProps[legacyProbe](json.RawMessage(`{"label":"新文案","buttonText":"旧文案"}`))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.Label != "新文案" {
		t.Errorf("应以新键为准，得到 %q —— 反过来会让作者的修改被历史残留覆盖", got.Label)
	}
}

// TestLegacyKeyTreatsEmptyNewValueAsMissing 新键是空串时，旧值接管。
//
// 改名前的检查器允许清空按钮文案，那种文档存的是 `"buttonText": ""` 或
// `"label": ""`。空串等价于「没有值」，不该挡住旧值。
func TestLegacyKeyTreatsEmptyNewValueAsMissing(t *testing.T) {
	got, err := DecodeProps[legacyProbe](json.RawMessage(`{"label":"","buttonText":"旧文案"}`))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.Label != "旧文案" {
		t.Errorf("新键为空串时应回落到旧值，得到 %q", got.Label)
	}
}

// TestLegacyKeyNoOpOnCleanDocument 没有旧键时原样通过（绝大多数节点走这条路）。
func TestLegacyKeyNoOpOnCleanDocument(t *testing.T) {
	got, err := DecodeProps[legacyProbe](json.RawMessage(`{"label":"新文案"}`))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if got.Label != "新文案" {
		t.Errorf("干净文档被改动了：%q", got.Label)
	}
	// 空 props 不得报错（大量容器节点是空的）。
	if _, err := DecodeProps[legacyProbe](nil); err != nil {
		t.Errorf("空 props 不该报错: %v", err)
	}
}

// TestApplyLegacyKeysDropsOldKey 改写后的 raw 里不得残留旧键。
//
// 为什么重要：残留的旧键会被下游的检查器 schema 当成一个未知字段
// （`jsonx.RejectUnknownMembers` 会拒绝它），或原样写回保存 —— 两种都是缺陷。
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
