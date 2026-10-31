package migrations_test

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func Test_634_MenuDedupAndMembershipPlacement(t *testing.T) {
	assertMenuDuplicationRootCauseFixed(t)

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("首轮 RunSeeds 失败（634 会在这里真执行）：%v", err)
	}
	assertInventoryMenuUnique(t, db)
	assertMembershipUnderCustomer(t, db)

	before := menuSnapshot(t, db)
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("第二轮 RunSeeds 失败：%v", err)
	}
	if after := menuSnapshot(t, db); after != before {
		t.Fatalf("634 不幂等：第二轮 seed 后菜单快照变了\n首轮: %s\n二轮: %s", before, after)
	}
}

func assertMenuDuplicationRootCauseFixed(t *testing.T) {
	t.Helper()

	var split migrations.Migration
	found := false
	for _, m := range migrations.All() {
		if m.Version == "229-inventory-menu-split" {
			split, found = m, true
		}
	}
	if !found {
		t.Fatal("229-inventory-menu-split 未注册：本用例的前提（那条迁移的门槛）没了")
	}
	if strings.Contains(split.CheckSQL, "WHERE path = '/admin/inventory/warehouses'") {
		t.Fatalf("229 的判据又回到「只看仓库页那一行存在」：224 按权限码把它的 path 改回 "+
			"/admin/inventory 后判据失配 → 整条重跑 → 再插一套菜单。判据：%s", split.CheckSQL)
	}
	if !strings.Contains(split.CheckSQL, "title = '库存'") {
		t.Fatalf("229 的判据应以「库存」目录为门槛（224 不碰 type=1 行）：%s", split.CheckSQL)
	}
	if !strings.Contains(split.SQL, "COALESCE(parent_id, 0) = 0 AND title = '库存'") {
		t.Error("229 的目录 INSERT 缺 NOT EXISTS 守卫：判据一旦失配就复制第二个「库存」目录")
	}
	for _, p := range []string{"/admin/inventory/warehouses", "/admin/inventory/reasons"} {
		if !strings.Contains(split.SQL, "x.path = '"+p+"'") {
			t.Errorf("229 的 %s 插入缺 NOT EXISTS 守卫：重跑会复制同名菜单行", p)
		}
	}

	rebuild := findSeedByVersion(t, "224-admin-nav-menu-rebuild")
	const exception = "NOT (v.code = 'inventory:warehouse_list' AND m.path = '/admin/inventory/warehouses')"
	if !strings.Contains(rebuild.SQL, exception) {
		t.Fatalf("224 的权限码映射缺仓库页例外：它会把仓库页的 path 改回 /admin/inventory，\n"+
			"侧栏出现两条库存管理、仓库页消失，且 229 的判据随之失配。期望包含：%s", exception)
	}
}

type menuRow struct {
	Title     string
	SortOrder int
	ParentID  uint64
}

func assertInventoryMenuUnique(t *testing.T, db *gorm.DB) {
	t.Helper()

	var dirIDs []uint64
	if err := db.Raw("SELECT id FROM sys_menus WHERE deleted_at IS NULL AND type = 1 " +
		"AND COALESCE(parent_id, 0) = 0 AND title = '库存'").Scan(&dirIDs).Error; err != nil {
		t.Fatalf("读「库存」目录失败：%v", err)
	}
	if len(dirIDs) != 1 {
		t.Fatalf("一级目录「库存」应唯一，实际 %d 条（%v）—— 侧栏会出现两个库存入口", len(dirIDs), dirIDs)
	}
	dirID := dirIDs[0]

	want := []struct {
		path  string
		title string
		sort  int
	}{
		{"/admin/inventory", "库存管理", 1},
		{"/admin/inventory/warehouses", "仓库管理", 2},
		{"/admin/inventory/sources", "货源管理", 3},
		{"/admin/inventory/purchases", "采购入库", 4},
		{"/admin/inventory/reasons", "变动原因字典", 5},
	}
	for _, w := range want {
		var rows []menuRow
		if err := db.Raw("SELECT title, sort_order, COALESCE(parent_id, 0) AS parent_id FROM sys_menus "+
			"WHERE deleted_at IS NULL AND type = 2 AND path = ?", w.path).Scan(&rows).Error; err != nil {
			t.Fatalf("读 %s 失败：%v", w.path, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s 应只有一条菜单，实际 %d 条（%+v）", w.path, len(rows), rows)
		}
		if rows[0].Title != w.title {
			t.Errorf("%s 的标题应为 %q，实际 %q", w.path, w.title, rows[0].Title)
		}
		if rows[0].SortOrder != w.sort {
			t.Errorf("%s 的排序应为 %d，实际 %d", w.path, w.sort, rows[0].SortOrder)
		}
		if rows[0].ParentID != dirID {
			t.Errorf("%s 应挂在「库存」目录 %d 下，实际父 id %d", w.path, dirID, rows[0].ParentID)
		}
	}

	var extra int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_menus WHERE deleted_at IS NULL AND type = 2 "+
		"AND parent_id = ? AND path NOT IN (?, ?, ?, ?, ?)",
		dirID, "/admin/inventory", "/admin/inventory/warehouses", "/admin/inventory/sources",
		"/admin/inventory/purchases", "/admin/inventory/reasons").Scan(&extra).Error; err != nil {
		t.Fatalf("读库存目录下的多余菜单失败：%v", err)
	}
	if extra != 0 {
		t.Errorf("库存目录下还有 %d 条计划外菜单（重复行或不该挂在这里的页面）", extra)
	}
}

func assertMembershipUnderCustomer(t *testing.T, db *gorm.DB) {
	t.Helper()

	var customerIDs []uint64
	if err := db.Raw("SELECT id FROM sys_menus WHERE deleted_at IS NULL AND type = 1 " +
		"AND COALESCE(parent_id, 0) = 0 AND title = '客户'").Scan(&customerIDs).Error; err != nil {
		t.Fatalf("读「客户」目录失败：%v", err)
	}
	if len(customerIDs) != 1 {
		t.Fatalf("一级目录「客户」应唯一，实际 %d 条（%v）", len(customerIDs), customerIDs)
	}

	want := []struct {
		path string
		sort int
	}{
		{"/admin/membership", 5},
		{"/admin/membership/assignments", 6},
	}
	for _, w := range want {
		var rows []menuRow
		if err := db.Raw("SELECT title, sort_order, COALESCE(parent_id, 0) AS parent_id FROM sys_menus "+
			"WHERE deleted_at IS NULL AND type = 2 AND path = ?", w.path).Scan(&rows).Error; err != nil {
			t.Fatalf("读 %s 失败：%v", w.path, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s 应只有一条菜单，实际 %d 条", w.path, len(rows))
		}
		if rows[0].ParentID != customerIDs[0] {
			t.Errorf("%s 应挂在「客户」目录 %d 下（会员等级按消费额算，属于客户域），实际父 id %d",
				w.path, customerIDs[0], rows[0].ParentID)
		}
		if rows[0].SortOrder != w.sort {
			t.Errorf("%s 的排序应为 %d，实际 %d", w.path, w.sort, rows[0].SortOrder)
		}
	}
}

func menuSnapshot(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var s string
	if err := db.Raw("SELECT COALESCE(string_agg(id::text || ':' || title || ':' || " +
		"COALESCE(parent_id, 0)::text || ':' || type::text || ':' || COALESCE(path, '') || ':' || " +
		"sort_order::text, '|' ORDER BY id), '') FROM sys_menus " +
		"WHERE deleted_at IS NULL AND type IN (1, 2)").Scan(&s).Error; err != nil {
		t.Fatalf("读菜单快照失败：%v", err)
	}
	return s
}
