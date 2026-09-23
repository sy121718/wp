package productservice

import (
	"testing"
)

// TestParseCollectionFilterMultiBrand 多品牌维度解析与「多值优先」。
func TestParseCollectionFilterMultiBrand(t *testing.T) {
	a := "11111111-1111-1111-1111-111111111111"
	b := "22222222-2222-2222-2222-222222222222"
	cases := []struct {
		name   string
		filter map[string]string
		want   []string
		single string
	}{
		{name: "多值", filter: map[string]string{"brandIds": a + "," + b}, want: []string{a, b}},
		{name: "多值优先于单值", filter: map[string]string{"brandId": a, "brandIds": b}, want: []string{b}},
		{name: "只有单值", filter: map[string]string{"brandId": a}, single: a},
		{name: "空值不下推", filter: map[string]string{"brandIds": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parseCollectionFilter(tc.filter)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if len(f.BrandIDs) != len(tc.want) {
				t.Fatalf("BrandIDs = %v，期望 %v", f.BrandIDs, tc.want)
			}
			for i := range tc.want {
				if f.BrandIDs[i] != tc.want[i] {
					t.Fatalf("BrandIDs = %v，期望 %v", f.BrandIDs, tc.want)
				}
			}
			if f.BrandID != tc.single {
				t.Fatalf("BrandID = %q，期望 %q", f.BrandID, tc.single)
			}
		})
	}
}
