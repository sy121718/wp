package buildservice

import (
	"testing"

	buildmodel "go_wp/internal/module/build/model"
)

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
