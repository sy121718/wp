package productenums

// product_skip_fallbacks_test.go — 变体清单保存「跳过原因」的中文原文守卫。
//
// 跳过原因不是业务错误（取值过不了 pkg/facing.IsBusinessKey），所以不进 ProductErrFallbacks；
// VariantSkipFallbacks 是它们的唯一真源，漏一条页面上显示的就是裸 key
// （VariantSkipHasMovement 这种串，运营只会以为后台坏了）。
//
// 两个方向都判失败：常量没有原文 / 表里登记了已改名或不属于本包的 key。

import (
	"strings"
	"testing"
)

// TestVariantSkipFallbacksCoverEnumsKeys 每条 VariantSkip* 原因常量都要有原文。
func TestVariantSkipFallbacksCoverEnumsKeys(t *testing.T) {
	consts := errKeyConstants(t)
	covered := 0
	for val, name := range consts {
		if !strings.HasPrefix(name, "VariantSkip") {
			continue
		}
		covered++
		if text := strings.TrimSpace(VariantSkipFallbacks[val]); text == "" {
			t.Errorf("%s (=%q) 没有中文兜底，页面上会显示裸 key", name, val)
		} else if text == val {
			t.Errorf("%s (=%q) 的兜底就是 key 本身 —— 等于没有兜底", name, val)
		}
	}
	if covered == 0 {
		t.Fatal("没扫到任何跳过原因常量 —— 守卫本身失效了")
	}
	// 反向：表里的 key 必须仍是本包声明的常量（改名后没同步 → 这张表退化成死条目）。
	for key := range VariantSkipFallbacks {
		if _, ok := consts[key]; !ok {
			t.Errorf("兜底表里登记了不属于 product enums 的 key %q（改名后的旧 key？）", key)
		}
	}
}
