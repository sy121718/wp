package builder

import (
	"errors"
	"context"
	_ "embed"
	"fmt"

	json "go_wp/pkg/jsonx"
)

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

//go:embed presets/user-account.v1.json
var userAccountV1 []byte

//go:embed presets/commerce-orders.v1.json
var commerceOrdersV1 []byte

type businessPresetEntry struct {
	meta     BusinessPreset
	document []byte
}

var businessPresetEntries = []businessPresetEntry{{
	meta: BusinessPreset{PresetKey: "commerce.cart-checkout", PresetVersion: 1,
		Label: "购物车与结算", Description: "完整的商品确认、收货信息与订单结果流程。", Category: "commerce", Kind: "block"},
	document: commerceCartCheckoutV1,
}, {
	meta: BusinessPreset{PresetKey: "user.account", PresetVersion: 1,
		Label: "账号中心", Description: "登录注册、找回密码、资料与登录设备的一体账号流程。", Category: "account", Kind: "block"},
	document: userAccountV1,
}, {
	meta: BusinessPreset{PresetKey: "commerce.orders", PresetVersion: 1,
		Label: "订单与售后", Description: "账号登录、订单详情、退货申请与售后进度的完整流程。", Category: "commerce", Kind: "block"},
	document: commerceOrdersV1,
}}

func BusinessPresets() []BusinessPreset {
	out := make([]BusinessPreset, len(businessPresetEntries))
	for i, entry := range businessPresetEntries {
		out[i] = entry.meta
	}
	return out
}

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
	return nil, ErrPresetUnavailable
}

// ErrPresetUnavailable 表示受控预设不存在：工具自有事实，
// 业务侧按需映射到自己的错误声明。
var ErrPresetUnavailable = errors.New("受控预设不可用")
