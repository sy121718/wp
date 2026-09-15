package dashboardhttp

// inspector_range_test.go — 区间列表控件的值解析（审计 EDT-007）。
//
// 面板不是校验入口：它只负责把既有的逗号分隔串显示成可编辑的行，再把行拼回同一格式。
// 因此这里的重点是「不丢东西」——认不出的片段原样保留，让作者看见并自己改，
// 静默丢弃会让「我明明写了档位，保存后没了」这种问题极难定位。

import (
	"reflect"
	"testing"
)

func TestParseRangeRows(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []inspectorRangeRow
	}{
		{"空值", "", nil},
		{"单档位", "0-199", []inspectorRangeRow{{Min: "0", Max: "199"}}},
		{"多档位含以上", "0-199,200-399,799+", []inspectorRangeRow{
			{Min: "0", Max: "199"}, {Min: "200", Max: "399"}, {Min: "799"},
		}},
		{"空白与空片段", " 0-199 , , 799+ ", []inspectorRangeRow{{Min: "0", Max: "199"}, {Min: "799"}}},
		// 认不出的片段原样保留（放在 Min 里），不丢弃：面板显示原文，构建期再按规则报错。
		{"非法片段保留", "abc", []inspectorRangeRow{{Min: "abc"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRangeRows(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseRangeRows(%q) = %+v，期望 %+v", tc.raw, got, tc.want)
			}
		})
	}
}
