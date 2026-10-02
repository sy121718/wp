package sysconfigservice

// sysconfig_dict_cache_internal_test.go — 国家名索引的**失效判据**。
//
// 包内测试（不是 _test 包）：countryLabelEntry.valid 是包内私有。**刻意不 import
// public/test/support** —— 本包被 internal/routers 依赖，而 support 会 import routers，
// 包内测试一旦拉进 support 就成 import cycle（本目录那个 _test 包正是为此存在的）。
// 所以这里只测不碰数据库的那一层：过期判据本身。Service 的查库与重建路径由同目录
// sysconfig_dict_test.go 的用例覆盖（它们每次读都经过 countryIndex）。

import (
	"testing"
	"time"
)

// TestCountryLabelEntryValidity 过期即失效、未过期即可用。
//
// 为什么这条重要：缓存如果只判「有没有」、不判「过没过期」，就退化成永久缓存 ——
// 字典改了必须重启进程才生效，而那种问题的表现是「页面看着正常、就是不改」，
// 是最难被当成故障报上来的形态。
func TestCountryLabelEntryValidity(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		expires time.Time
		want    bool
	}{
		{name: "未过期可用", expires: now.Add(time.Minute), want: true},
		{name: "已过期失效", expires: now.Add(-time.Second), want: false},
		// 边界：恰好等于 now 按失效处理（now.Before(now) 为 false）。取这个方向是刻意的 ——
		// 宁可多重算一次索引（一次 247 行的读），也不要多用一个可能已经过期的旧名。
		{name: "正好到期按失效处理", expires: now, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := countryLabelEntry{expires: tt.expires}
			if got := entry.valid(now); got != tt.want {
				t.Fatalf("valid(expires=%v, now=%v) = %v，期望 %v", tt.expires, now, got, tt.want)
			}
		})
	}
}
