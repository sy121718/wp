package productservice

// bundle_items_validate_test.go — 商品写路径上 bundle_items 的规则校验。
//
// 为什么这里也要校验（审查 P1）：页面上的捆绑配置走 /bundle/set，那条路径有
// validateBundleConfig 做完整校验；但 MCP / AI 可以直接调 UpdateReq.BundleItems
// 写进同一列。原先商品写路径只归一形状，于是下面这些自相矛盾的配置能存进去 ——
// 一旦保存，前台任何选择都会被拒，等于把商品锁死。

import (
	"encoding/json"
	"testing"

	productenums "go_wp/internal/module/product/enums"
)

func bundleJSON(t *testing.T, maxOptions, minTotal, maxTotal int) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"maxOptions": maxOptions, "minTotalQty": minTotal, "maxTotalQty": maxTotal, "options": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNormalizeBundleItemsRejectsSelfContradictingRange 整单下限高于上限必须拒绝。
func TestNormalizeBundleItemsRejectsSelfContradictingRange(t *testing.T) {
	if _, err := normalizeBundleItems(bundleJSON(t, 3, 5, 1)); err == nil {
		t.Fatal("minTotalQty(5) > maxTotalQty(1) 应被拒绝：这种配置保存后前台永远无法下单")
	}
}

// TestNormalizeBundleItemsRejectsOutOfRangeMaxOptions 超过服务端护栏必须拒绝。
func TestNormalizeBundleItemsRejectsOutOfRangeMaxOptions(t *testing.T) {
	if _, err := normalizeBundleItems(bundleJSON(t, 9999, 0, 0)); err == nil {
		t.Fatal("maxOptions 超过上限应被拒绝")
	}
}

// TestNormalizeBundleItemsKeepsValidConfig 合法配置原样通过（不能因为加了校验就误拒）。
func TestNormalizeBundleItemsKeepsValidConfig(t *testing.T) {
	out, err := normalizeBundleItems(bundleJSON(t, 3, 1, 5))
	if err != nil {
		t.Fatalf("合法配置不该被拒：%v", err)
	}
	var cfg map[string]any
	if uerr := json.Unmarshal(out, &cfg); uerr != nil {
		t.Fatalf("输出不是合法 JSON：%v", uerr)
	}
	if cfg["minTotalQty"] != float64(1) || cfg["maxTotalQty"] != float64(5) {
		t.Fatalf("区间被改动：%+v", cfg)
	}
}

// TestNormalizeBundleItemsEmptyStaysEmpty 空 / [] / null 仍走「无捆绑」，不因新校验变红。
func TestNormalizeBundleItemsEmptyStaysEmpty(t *testing.T) {
	for _, raw := range []string{"", "[]", "null"} {
		if _, err := normalizeBundleItems(json.RawMessage(raw)); err != nil {
			t.Errorf("%q 应被接受（历史列默认值），实际 %v", raw, err)
		}
	}
}

var _ = productenums.ErrBundleQtyInvalid
