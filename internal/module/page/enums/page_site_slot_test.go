package pageenums

import (
	"sort"
	"testing"
)

// migration138SiteSlotCheck 与 public/migrations/138_page_site_slots.sql 的
// ck_page_site_slots_slot CHECK 逐字对齐；新增槽位必须同时改迁移与 SiteSlotDefs。
var migration138SiteSlotCheck = []string{
	SiteSlotShop,
	SiteSlotBlog,
	SiteSlotCart,
	SiteSlotCheckout,
	SiteSlotAccount,
	SiteSlotLogin,
	SiteSlotRegister,
	SiteSlotForgot,
	SiteSlotReset,
	SiteSlotOrders,
}

// TestSiteSlotDefsMatchMigration138Check SiteSlotDefs 与迁移 138 DDL CHECK 一致。
func TestSiteSlotDefsMatchMigration138Check(t *testing.T) {
	if len(SiteSlotDefs) != len(migration138SiteSlotCheck) {
		t.Fatalf("SiteSlotDefs 数量 %d 与迁移 138 CHECK %d 不一致", len(SiteSlotDefs), len(migration138SiteSlotCheck))
	}
	for i, def := range SiteSlotDefs {
		if def.Key != migration138SiteSlotCheck[i] {
			t.Fatalf("顺序/键不一致：SiteSlotDefs[%d]=%q migration138[%d]=%q", i, def.Key, i, migration138SiteSlotCheck[i])
		}
	}
	keys := make([]string, 0, len(SiteSlotDefs))
	for _, def := range SiteSlotDefs {
		keys = append(keys, def.Key)
	}
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Fatalf("SiteSlotDefs 出现重复键: %q", sorted[i])
		}
	}
}
