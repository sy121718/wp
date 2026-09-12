package source

// item_test.go — 集合项访问器的语义测试（issue #35）。
//
// 重点不在「能取值」，而在**「缺失 ≠ 零值」**这条本项目反复依赖的语义：
// 「没有启用变体」（minPrice 缺失）与「0 元」、「尚无评分」（ratingValue 缺失）与「0 分」
// 是两回事。访问器一旦把缺失当零值返回 ok=true，排序与筛选就会悄悄改变行为。

import (
	"testing"
	"time"
)

func TestItemFloatDistinguishesMissingFromZero(t *testing.T) {
	// 值为 0：取到了，ok=true —— 调用方不该把它当「没有值」。
	if v, ok := ItemFloat(map[string]any{ItemFieldMinPrice: 0.0}, ItemFieldMinPrice); !ok || v != 0 {
		t.Fatalf("值为 0 应取到: v=%v ok=%v", v, ok)
	}
	// 键不存在：ok=false —— 这才是「没有启用变体 / 尚无评分」。
	if v, ok := ItemFloat(map[string]any{}, ItemFieldMinPrice); ok || v != 0 {
		t.Fatalf("缺失应为 ok=false: v=%v ok=%v", v, ok)
	}
	// 键在但类型不是数值：同样 ok=false（构建期数据形状不对，不该被当作有效值）。
	if _, ok := ItemFloat(map[string]any{ItemFieldMinPrice: "12.00"}, ItemFieldMinPrice); ok {
		t.Fatalf("字符串不该被当成数值取到（格式化字符串是另一个字段口径）")
	}
}

func TestItemFloatAcceptsNumericKinds(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  any
		want float64
	}{
		{"float64", 12.5, 12.5},
		{"float32", float32(3.5), 3.5},
		{"int", 7, 7},
		{"int64", int64(9), 9},
	} {
		if v, ok := ItemFloat(map[string]any{ItemFieldMinPrice: tc.raw}, ItemFieldMinPrice); !ok || v != tc.want {
			t.Fatalf("%s: 期望 %v, 实际 v=%v ok=%v", tc.name, tc.want, v, ok)
		}
	}
}

func TestItemTimeParsesRFC3339AndRejectsBadInput(t *testing.T) {
	want := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if got, ok := ItemTime(map[string]any{ItemFieldCreatedAt: want.Format(time.RFC3339)}, ItemFieldCreatedAt); !ok || !got.Equal(want) {
		t.Fatalf("应解析 RFC3339: got=%v ok=%v", got, ok)
	}
	// 空、非法、缺失一律 ok=false（调用方据此判定「没有可排序的时间」，
	// 而不是把零值当成 0001 年参与比较）。
	for _, bad := range []any{"", "  ", "2026/03/01", nil} {
		if got, ok := ItemTime(map[string]any{ItemFieldCreatedAt: bad}, ItemFieldCreatedAt); ok || !got.IsZero() {
			t.Fatalf("非法时间 %#v 应返回零值 + false，实际 %v / %v", bad, got, ok)
		}
	}

	if _, ok := ItemTime(map[string]any{}, ItemFieldCreatedAt); ok {
		t.Fatalf("缺失时间应为 ok=false")
	}
}

func TestItemStringReturnsEmptyForWrongType(t *testing.T) {
	if v := ItemString(map[string]any{ItemFieldSlug: "tee"}, ItemFieldSlug); v != "tee" {
		t.Fatalf("应取到字符串: %q", v)
	}
	if v := ItemString(map[string]any{ItemFieldSlug: 42}, ItemFieldSlug); v != "" {
		t.Fatalf("类型不符应返回空串: %q", v)
	}
}
