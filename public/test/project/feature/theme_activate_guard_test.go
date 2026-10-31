package feature

import (
	"context"
	"errors"
	"testing"
	"time"

	projectmodel "go_wp/internal/module/project/model"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

func newProjectThemeModel(t *testing.T) (*gorm.DB, *projectmodel.Model) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	return db, projectmodel.NewProjectModel(db)
}

func TestActivateThemeModelRollsBackOnMissingTarget(t *testing.T) {
	db, m := newProjectThemeModel(t)
	ctx := context.Background()

	const (
		projectID = "11111111-1111-1111-1111-111111111111"
		themeA    = "22222222-2222-2222-2222-222222222222"
		missed    = "33333333-3333-3333-3333-333333333333"
	)
	support.SeedProjectRow(t, db, projectID, "站点")
	if err := db.Exec("INSERT INTO themes (id, project_id, name, settings, is_active, create_time, update_time) VALUES (?, ?, '主题A', '{}', true, NOW(), NOW())", themeA, projectID).Error; err != nil {
		t.Fatalf("插入主题失败: %v", err)
	}

	err := m.ActivateTheme(ctx, projectID, missed, time.Now().UTC())
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("激活目标不存在应返回 ErrRecordNotFound 以触发回滚，got %v", err)
	}

	var active int64
	if err := db.Raw("SELECT COUNT(*) FROM themes WHERE project_id = ? AND is_active = true", projectID).
		Scan(&active).Error; err != nil {
		t.Fatalf("统计激活主题失败: %v", err)
	}
	if active != 1 {
		t.Errorf("事务必须回滚：原激活主题应保持激活，got active=%d", active)
	}
}
