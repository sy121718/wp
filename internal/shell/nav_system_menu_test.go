package shell_test

// nav_system_menu_test.go — 系统设置页的侧栏入口（Y1）。
//
// 菜单真源是 sys_menus 表（224 起侧栏由它驱动）。这条用例走**真库**读那一行，
// 再经真实的 BuildNav 渲染成侧栏结构 —— 只查 SQL 不看渲染，会漏掉「行在、但
// is_hidden=1 / type 不对 / 没有权限码所以过滤掉」这类「菜单存在却看不见」的情形。

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/shell"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestSystemSettingsMenuRendersInSidebar(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	// 迁移建菜单行（498）；seed 台账与它同批，这里直接跑一次保证行在。
	if err := runMigrations(t, db); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}

	var row struct {
		ID       uint64
		ParentID uint64
		Title    string
		Path     string
		Icon     string
		Type     int
		IsHidden int
	}
	if err := db.Raw(`SELECT id, parent_id, title, path, icon, type, is_hidden
	                     FROM sys_menus WHERE path = '/admin/system' AND deleted_at IS NULL`).
		Scan(&row).Error; err != nil {
		t.Fatalf("读菜单行失败: %v", err)
	}
	if row.ID == 0 {
		t.Fatal("迁移 498 之后 sys_menus 里仍没有 /admin/system")
	}
	parent := admindto.MenuTreeNode{ID: row.ParentID, Title: "站点", Type: 1}
	leaf := admindto.MenuTreeNode{
		ID: row.ID, ParentID: row.ParentID, Title: row.Title, Path: row.Path,
		Icon: row.Icon, Type: row.Type, IsHidden: row.IsHidden,
	}
	parent.Children = []admindto.MenuTreeNode{leaf}

	groups := shell.BuildNav([]admindto.MenuTreeNode{parent}, row.Path, func(key, fallback string) string {
		return fallback
	})
	var rendered strings.Builder
	for _, g := range groups {
		for _, n := range g.Nodes {
			rendered.WriteString(n.Path + " ")
		}
	}
	if !strings.Contains(rendered.String(), "/admin/system") {
		t.Fatalf("侧栏里没有系统设置入口：%s", rendered.String())
	}
	t.Logf("侧栏渲染：%s（菜单行 id=%d，父分组 %d，is_hidden=%d）",
		strings.TrimSpace(rendered.String()), row.ID, row.ParentID, row.IsHidden)
}

// runMigrations 跑一次结构迁移（菜单行由迁移 498 建）。
func runMigrations(t *testing.T, db *gorm.DB) error {
	t.Helper()
	if err := migrations.Run(db); err != nil {
		return err
	}
	// 菜单行由 **seed** 建（498 走 registerSeed）：结构迁移与 seed 是两个台账，
	// 只跑 Run 会得到「库里没有这一行」的假象。
	return migrations.RunSeeds(db)
}
