package viewtest

import (
	"strings"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
	"gorm.io/gorm"
)

func TestSystemSettingsMenuRendersInSidebar(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}

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

	items := navItemsOf(navNodes([]admindto.MenuTreeNode{parent}))
	var rendered strings.Builder
	for _, g := range items {
		for _, n := range g.Children {
			rendered.WriteString(n.Path + " ")
		}
	}
	if !strings.Contains(rendered.String(), "/admin/system") {
		t.Fatalf("侧栏里没有系统设置入口：%s", rendered.String())
	}
	t.Logf("侧栏渲染：%s（菜单行 id=%d，父分组 %d，is_hidden=%d）",
		strings.TrimSpace(rendered.String()), row.ID, row.ParentID, row.IsHidden)
}

func runMigrations(t *testing.T, db *gorm.DB) error {
	t.Helper()
	if err := migrations.Run(db); err != nil {
		return err
	}

	return migrations.RunSeeds(db)
}
