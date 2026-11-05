package retention

import (
	"strconv"
	"strings"
	"testing"
)

func TestCatalogCoversGrowthTables(t *testing.T) {
	want := []string{
		"page_views", "inventory_stock_movements", "mail_campaign_events", "mail_logs",
		"master_data_changes", "page_revisions", "page_artifacts", "artifacts 磁盘目录",
		"order_status_logs", "sys_translation", "mail_automation_node_logs",
		"product_ratings", "page_views_daily",

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

			if d.Retain <= 0 && !strings.Contains(d.Note, "可配") {
				t.Errorf("%s 声明为清理但没有保留期，且未说明是「按工程可配」", d.Table)
			}
			if strings.TrimSpace(d.Executor) == "" {
				t.Errorf("%s 声明为清理却没有执行者", d.Table)
			}
		case CleanupOrphan:

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

		if len(strings.TrimSpace(d.Note)) < 10 {
			t.Errorf("%s 的策略说明过于简略: %q", d.Table, d.Note)
		}
	}
}

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
