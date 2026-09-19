package buildservice

import (
	"testing"

	buildmodel "go_wp/internal/module/build/model"
)

// TestNormalizeIntent 构建意图的归一化白名单（与迁移 295 的 CHECK 一致）。
//
// 空值归到 dependency 是刻意的：当前唯一的入队来源就是依赖失效扇出，
// 默认值必须站在多数且更安全的一侧；白名单外的值必须在入队前被拒。
func TestNormalizeIntent(t *testing.T) {
	cases := []struct {
		raw     string
		want    string
		wantOK  bool
		comment string
	}{
		{raw: "", want: buildmodel.IntentDependency, wantOK: true, comment: "空值 = 依赖重建"},
		{raw: "dependency", want: buildmodel.IntentDependency, wantOK: true},
		{raw: "manual", want: buildmodel.IntentManual, wantOK: true},
		{raw: "  manual  ", want: buildmodel.IntentManual, wantOK: true, comment: "两侧空白被裁掉"},
		{raw: "whatever", wantOK: false},
		{raw: "MANUAL", wantOK: false, comment: "大小写不宽容（DDL 的 CHECK 同样不容）"},
	}
	for _, c := range cases {
		got, ok := normalizeIntent(c.raw)
		if ok != c.wantOK {
			t.Fatalf("normalizeIntent(%q) 的判定应为 %v，实际 %v（%s）", c.raw, c.wantOK, ok, c.comment)
		}
		if ok && got != c.want {
			t.Fatalf("normalizeIntent(%q) 应为 %q，实际 %q", c.raw, c.want, got)
		}
	}
}
