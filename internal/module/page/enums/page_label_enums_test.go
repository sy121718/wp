package pageenums

import (
	"testing"

	"go_wp/pkg/builder"
)

func TestSlotTokensMirrorBuilder(t *testing.T) {
	if slotKeyHeader != builder.SlotHeader {
		t.Fatalf("槽位键漂移：本包 %q，builder.SlotHeader %q", slotKeyHeader, builder.SlotHeader)
	}
	if slotKeyFooter != builder.SlotFooter {
		t.Fatalf("槽位键漂移：本包 %q，builder.SlotFooter %q", slotKeyFooter, builder.SlotFooter)
	}
}

func TestTranslationOriginOfSlot(t *testing.T) {
	cases := []struct {
		slot string
		want string
	}{
		{builder.SlotHeader, TranslationOriginHeader},
		{builder.SlotFooter, TranslationOriginFooter},
		{"", TranslationOriginBlock},
		{"announcement", TranslationOriginBlock},
	}
	for _, c := range cases {
		if got := TranslationOriginOfSlot(c.slot); got != c.want {
			t.Errorf("TranslationOriginOfSlot(%q) = %q，want %q", c.slot, got, c.want)
		}
	}
}

func TestLabelFunctionsAlwaysReturnText(t *testing.T) {
	for _, stage := range []string{RebuildStagePlan, RebuildStageBuild, "plan2"} {
		key, fallback := RebuildStageLabel(stage)
		if key == "" && fallback == "" {
			t.Errorf("RebuildStageLabel(%q) 返回了空事实", stage)
		}
	}

	if key, fallback := RebuildStageLabel(""); key != "" || fallback != "" {
		t.Errorf("空阶段应回落到原值（空串），实际 %q / %q", key, fallback)
	}
	for _, status := range []string{LangStatusExcluded, LangStatusPublished, LangStatusDraft, "unknown"} {
		key, fallback := LangStatusLabel(status)
		if key == "" || fallback == "" {
			t.Errorf("LangStatusLabel(%q) 缺 key 或兜底：%q / %q", status, key, fallback)
		}
	}
	for _, code := range []string{RedirectResultCreated, RedirectResultDeleted, RedirectResultMerged} {
		key, fallback := RedirectResultLabel(code)
		if key == "" || fallback == "" {
			t.Errorf("RedirectResultLabel(%q) 缺 key 或兜底：%q / %q", code, key, fallback)
		}
	}

	if _, fallback := RedirectResultLabel("created2"); fallback != "created2" {
		t.Errorf("未知结果码应回落到原值，实际 %q", fallback)
	}
}

func TestLangStatusOfPriority(t *testing.T) {
	cases := []struct {
		excluded, published bool
		want                string
	}{
		{false, false, LangStatusDraft},
		{false, true, LangStatusPublished},
		{true, false, LangStatusExcluded},
		{true, true, LangStatusExcluded},
	}
	for _, c := range cases {
		if got := LangStatusOf(c.excluded, c.published); got != c.want {
			t.Errorf("LangStatusOf(%v, %v) = %q，want %q", c.excluded, c.published, got, c.want)
		}
	}
}

func TestPagesBulkDeleteOutcome(t *testing.T) {
	for _, tc := range []struct {
		name             string
		deleted, skipped int
		wantKey          string
		wantArgs         int
	}{
		{"未勾选", 0, 0, BulkPageNoneSelected, 0},
		{"全成功", 3, 0, BulkPageAllDeleted, 1},
		{"全部失败", 0, 3, BulkPageAllSkipped, 1},
		{"部分成功", 2, 3, BulkPagePartial, 2},
	} {
		got := PagesBulkDeleteOutcome(tc.deleted, tc.skipped)
		if got.Key != tc.wantKey {
			t.Errorf("%s：应选 %s，实际 %s", tc.name, tc.wantKey, got.Key)
		}
		if got.Fallback == "" {
			t.Errorf("%s：必须有中文兜底（词条缺失时页面要仍可读）", tc.name)
		}
		if len(got.Args) != tc.wantArgs {
			t.Errorf("%s：应带 %d 个占位符实参，实际 %v", tc.name, tc.wantArgs, got.Args)
		}
	}
}

func TestRedirectBulkDeleteOutcome(t *testing.T) {
	for _, tc := range []struct {
		name             string
		deleted, skipped int
		wantKey          string
		wantArgs         int
	}{
		{"未勾选", 0, 0, BulkRedirectNoneSelected, 0},
		{"全成功", 3, 0, BulkRedirectAllDeleted, 1},
		{"全部失败", 0, 3, BulkRedirectAllSkipped, 1},
		{"部分成功", 2, 3, BulkRedirectPartial, 2},
	} {
		got := RedirectBulkDeleteOutcome(tc.deleted, tc.skipped)
		if got.Key != tc.wantKey {
			t.Errorf("%s：应选 %s，实际 %s", tc.name, tc.wantKey, got.Key)
		}
		if got.Fallback == "" {
			t.Errorf("%s：必须有中文兜底", tc.name)
		}
		if len(got.Args) != tc.wantArgs {
			t.Errorf("%s：应带 %d 个占位符实参，实际 %v", tc.name, tc.wantArgs, got.Args)
		}
	}
}
