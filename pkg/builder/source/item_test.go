package source

import (
	"testing"
	"time"
)

func TestItemFloatDistinguishesMissingFromZero(t *testing.T) {

	if v, ok := ItemFloat(map[string]any{ItemFieldMinPrice: 0.0}, ItemFieldMinPrice); !ok || v != 0 {
		t.Fatalf("值为 0 应取到: v=%v ok=%v", v, ok)
	}

	if v, ok := ItemFloat(map[string]any{}, ItemFieldMinPrice); ok || v != 0 {
		t.Fatalf("缺失应为 ok=false: v=%v ok=%v", v, ok)
	}

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
