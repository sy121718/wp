package pageenums

import (
	"testing"

	"go_wp/internal/builder"
)

// TestSlotTokensMirrorBuilder 槽位键镜像与 builder 的槽位常量一致。
//
// 为什么要有这条：本包不 import builder（enums 是纯文案的叶子包），槽位键在这里
// 抄了一份字面量。抄写会漂移，而漂移的表现是**静默错标签** —— builder 把页眉槽位改名后，
// TranslationOriginOfSlot 会把它判成「全局块」，页面上照样渲染、没有任何报错。
// 判据落在测试里而不是注释里：改坏了会变红。
func TestSlotTokensMirrorBuilder(t *testing.T) {
	if slotKeyHeader != builder.SlotHeader {
		t.Fatalf("槽位键漂移：本包 %q，builder.SlotHeader %q", slotKeyHeader, builder.SlotHeader)
	}
	if slotKeyFooter != builder.SlotFooter {
		t.Fatalf("槽位键漂移：本包 %q，builder.SlotFooter %q", slotKeyFooter, builder.SlotFooter)
	}
}

// TestTranslationOriginOfSlot 槽位 → 来源标签：两个槽位各有自己的标签，其余落「全局块」。
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

// TestLabelFunctionsAlwaysReturnText 三个标签函数：已知值给 key + 兜底，未知值也不返回
// 「空 key + 空兜底」那种「没有这条文案」的伪信号（调用点会把空当成「不用提示」）。
func TestLabelFunctionsAlwaysReturnText(t *testing.T) {
	for _, stage := range []string{RebuildStagePlan, RebuildStageBuild, "plan2"} {
		key, fallback := RebuildStageLabel(stage)
		if key == "" && fallback == "" {
			t.Errorf("RebuildStageLabel(%q) 返回了空事实", stage)
		}
	}
	// 空串不是值域成员（调用点先判「有没有失败痕迹」）：它回落到原值即空串，
	// 页面上只剩失败时刻 —— 与迁入前控制器里那个 switch 逐字同行为。
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
	// 未知结果码回落到原值（调用点只传本包常量，这一支不可达；真出现时页面上能指认它）。
	if _, fallback := RedirectResultLabel("created2"); fallback != "created2" {
		t.Errorf("未知结果码应回落到原值，实际 %q", fallback)
	}
}

// TestLangStatusOfPriority 状态派生：已排除优先于已发布（排除会同时下线产物，
// 两者不是同一时刻的状态，先报「已排除」才不诱导运营少做一步）。
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

// TestPagesBulkDeleteOutcome 页面批量删除的四个分支：选哪一条 + 带哪几个计数。
//
// 判据在 enums 侧（控制器只调用一行）：分支选错的表现是「部分成功被报成全成功」——
// 用户不会再去看剩下那几个，而页面上看不出任何异常。
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

// TestRedirectBulkDeleteOutcome 重定向批量删除的四个分支（判据同页面侧，文案域不同）。
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
