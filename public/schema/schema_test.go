package schema

import "testing"

// order.txt 必须恰好覆盖「有系统数据段」的表，且不得指向不存在的表文件：
// 少一张 → 该表系统数据静默不装载；多一张 → 启动时 panic 或读到空段。
func TestOrderCoversEveryDataTable(t *testing.T) {
	files := tableFiles()
	have := map[string]bool{}
	withData := map[string]bool{}
	for _, name := range files {
		table := name[len("tables/") : len(name)-len(".sql")]
		have[table] = true
		if sectionOf(mustRead(name), "data") != "" {
			withData[table] = true
		}
	}
	ordered := map[string]bool{}
	for _, table := range order() {
		if !have[table] {
			t.Errorf("order.txt 指向不存在的表：%s", table)
			continue
		}
		if ordered[table] {
			t.Errorf("order.txt 重复登记：%s", table)
		}
		ordered[table] = true
	}
	for table := range withData {
		if !ordered[table] {
			t.Errorf("表 %s 有系统数据但不在 order.txt 里", table)
		}
	}
	for table := range ordered {
		if !withData[table] {
			t.Errorf("order.txt 登记了没有系统数据的表：%s", table)
		}
	}
}

func TestDigestAndVersionAreStable(t *testing.T) {
	first, second := Digest(), Digest()
	if first != second {
		t.Fatalf("摘要不稳定：%s vs %s", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("摘要长度应为 64，实际 %d", len(first))
	}
	if got, want := Version(), "schema-"+first[:8]; got != want {
		t.Fatalf("版本号与摘要不同源：%s vs %s", got, want)
	}
}
