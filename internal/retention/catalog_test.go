package retention

// catalog_test.go — 生命周期声明的完整性（审计 IDX-019）。
//
// 这份目录的价值全在「完整性」：漏一张表就等于回到零声明的状态，
// 而漏掉的恰好是最会增长的那张时，没人会注意到。测试因此钉死三件事：
// 审计列出的表都在、声明之间不自相矛盾、每个条目都写明了理由。

import (
	"strconv"
	"strings"
	"testing"
)

// TestCatalogCoversGrowthTables 审计 IDX-019 列出的表一张都不能漏。
func TestCatalogCoversGrowthTables(t *testing.T) {
	want := []string{
		"page_views", "inventory_stock_movements", "mail_campaign_events", "mail_logs",
		"master_data_changes", "page_revisions", "page_artifacts", "artifacts 磁盘目录",
		"order_status_logs", "sys_translation", "mail_automation_node_logs",
		"product_ratings", "page_views_daily", "page_schedules",
		// content_objects（审计 IDX-016）：实现早就在（artifact 的内容对象 GC，
		// 挂在 page 每日任务的产物 GC 上），声明与这里的 want 却都没有它 ——
		// 于是「新增增长表必须声明保留期」这条不变量对这张表静默失效。
		"content_objects",
	}
	for _, table := range want {
		if _, ok := DeclarationOf(table); !ok {
			t.Errorf("生命周期目录缺少 %s 的声明（审计 IDX-019）", table)
		}
	}
	if len(Declarations()) < len(want) {
		t.Fatalf("声明条数应不少于审计清单: %d < %d", len(Declarations()), len(want))
	}
}

// TestFormatDeclarationsListsEverything 读出口（cmd/retention-catalog）必须列出**全部**声明：
// 少列一张，等于那句话（「运维手册从这里取」）对那张表不成立，而运维不会知道自己漏看了什么。
func TestFormatDeclarationsListsEverything(t *testing.T) {
	out := FormatDeclarations()
	if !strings.Contains(out, "共 "+strconv.Itoa(len(Declarations()))+" 张表") {
		t.Errorf("读出口的抬头应给出声明条数：\n%s", out)
	}
	for _, d := range Declarations() {
		if !strings.Contains(out, "· "+d.Table) {
			t.Errorf("读出口缺少 %s 的条目", d.Table)
		}
		if !strings.Contains(out, d.Executor) {
			t.Errorf("读出口缺少 %s 的执行者", d.Table)
		}
	}
}

// TestCatalogDeclarationsAreConsistent 声明之间不能自相矛盾。
func TestCatalogDeclarationsAreConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Declarations() {
		if strings.TrimSpace(d.Table) == "" {
			t.Fatal("声明缺少表名")
		}
		if seen[d.Table] {
			t.Fatalf("表 %s 有重复声明", d.Table)
		}
		seen[d.Table] = true
		switch d.Kind {
		case CleanupDelete:
			// 保留期可以是 0 —— 但只有一种合法解释：保留期由运行时配置决定
			//（page_views 按工程可配）。这种条目必须在说明里写清是「可配」，
			// 否则「0 天保留期」看起来就是「立刻全删」。
			if d.Retain <= 0 && !strings.Contains(d.Note, "可配") {
				t.Errorf("%s 声明为清理但没有保留期，且未说明是「按工程可配」", d.Table)
			}
			if strings.TrimSpace(d.Executor) == "" {
				t.Errorf("%s 声明为清理却没有执行者", d.Table)
			}
		case CleanupOrphan:
			// 按引用清理：保留期必须为 0。给了保留期就等于说「按时间删」，
			// 那正是这个类型要避免的事（会删掉仍在用的行）。
			if d.Retain != 0 {
				t.Errorf("%s 声明为按引用清理却给了保留期（%s）", d.Table, d.Retain)
			}
			if strings.TrimSpace(d.Executor) == "" {
				t.Errorf("%s 声明为按引用清理却没有执行者", d.Table)
			}
		case CleanupNone:
			if d.Retain != 0 {
				t.Errorf("%s 声明为不清理却给了保留期（%s）", d.Table, d.Retain)
			}
		default:
			t.Errorf("%s 的清理方式非法: %q", d.Table, d.Kind)
		}
		// 每个条目都要有说明：目录的意义正是「为什么留这么久 / 为什么不清理」。
		if len(strings.TrimSpace(d.Note)) < 10 {
			t.Errorf("%s 的策略说明过于简略: %q", d.Table, d.Note)
		}
	}
}

// TestDeclarationsAreCopy 目录对外是只读拷贝。
func TestDeclarationsAreCopy(t *testing.T) {
	first := Declarations()
	if len(first) == 0 {
		t.Fatal("目录不应为空")
	}
	first[0].Table = "__mutated__"
	if Declarations()[0].Table == "__mutated__" {
		t.Fatal("Declarations 返回了内部切片，调用方可以篡改声明")
	}
}
