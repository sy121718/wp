package productenums

import (
	"strings"
	"testing"
)

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

	for key := range VariantSkipFallbacks {
		if _, ok := consts[key]; !ok {
			t.Errorf("兜底表里登记了不属于 product enums 的 key %q（改名后的旧 key？）", key)
		}
	}
}
