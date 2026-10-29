package builder

import (
	"context"
	_ "embed"
	"fmt"

	blockcontract "go_wp/internal/module/block/contract"
	json "go_wp/pkg/jsonx"
)

// BusinessPreset 是受控预设目录声明。编辑器只拿来源和说明，不拿可覆写的权威Document。
type BusinessPreset struct {
	PresetKey     string `json:"presetKey"`
	PresetVersion int    `json:"presetVersion"`
	Label         string `json:"label"`
	Description   string `json:"description,omitempty"`
	Category      string `json:"category,omitempty"`
	Kind          string `json:"kind,omitempty"`
}

//go:embed presets/commerce-cart-checkout.v1.json
var commerceCartCheckoutV1 []byte

type businessPresetEntry struct {
	meta     BusinessPreset
	document []byte
}

var businessPresetEntries = []businessPresetEntry{{
	meta: BusinessPreset{PresetKey: "commerce.cart-checkout", PresetVersion: 1,
		Label: "购物车与结算", Description: "完整的商品确认、收货信息与订单结果流程。", Category: "commerce", Kind: "block"},
	document: commerceCartCheckoutV1,
}}

// BusinessPresets 与ResolvePreset共享同一清单，返回独立元数据副本。
func BusinessPresets() []BusinessPreset {
	out := make([]BusinessPreset, len(businessPresetEntries))
	for i, entry := range businessPresetEntries {
		out[i] = entry.meta
	}
	return out
}

// BusinessPresetRegistry 按固定来源版本提供Document；不读取控制面或运行时全局状态。
type BusinessPresetRegistry struct{}

func (BusinessPresetRegistry) ResolvePreset(_ context.Context, key string, version int) (json.RawMessage, error) {
	for _, entry := range businessPresetEntries {
		if entry.meta.PresetKey == key && entry.meta.PresetVersion == version {
			page, err := ParsePage(entry.document)
			if err != nil {
				return nil, fmt.Errorf("受控预设%s版本%d解析失败: %w", key, version, err)
			}
			if err := ValidatePage(page); err != nil {
				return nil, fmt.Errorf("受控预设%s版本%d无效: %w", key, version, err)
			}
			return append(json.RawMessage(nil), entry.document...), nil
		}
	}
	return nil, blockcontract.ErrPresetUnavailable
}
