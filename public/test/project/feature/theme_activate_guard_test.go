package feature

// theme_activate_guard_test.go — model 层「激活目标不存在必须回滚」的回归测试。
//
// service 层的 GetTheme 会先挡掉大部分「目标不存在」，真正危险的是竞态：
// GetTheme 通过之后目标被并发删除 —— 事务第一步已把同工程其余主题全部取消激活，
// 第二步 UPDATE 匹配 0 行若被忽略，事务照样提交，整个工程落入 is_active 全 false，
// 而 API 回报「激活成功」。这里绕过 service 直接打 model，覆盖那条守卫分支。

import (
	"context"
	"errors"
	"testing"
	"time"

	projectmodel "go_wp/internal/module/project/model"

	"go_wp/public/test/support"
	"gorm.io/gorm"
)

// newProjectThemeModel 建隔离 PG schema + projects/themes 两表，返回裸 model。
func newProjectThemeModel(t *testing.T) (*gorm.DB, *projectmodel.Model) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil, nil
	}
	for _, statement := range []string{
		"CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, settings JSON NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)",
		"CREATE TABLE themes (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, settings JSON NOT NULL, is_active BOOLEAN NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
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
	if err := db.Exec("INSERT INTO projects (id, name, settings, created_at, updated_at) VALUES (?, '站点', '{}', NOW(), NOW())", projectID).Error; err != nil {
		t.Fatalf("插入工程失败: %v", err)
	}
	if err := db.Exec("INSERT INTO themes (id, project_id, name, settings, is_active, created_at, updated_at) VALUES (?, ?, '主题A', '{}', true, NOW(), NOW())", themeA, projectID).Error; err != nil {
		t.Fatalf("插入主题失败: %v", err)
	}

	// 目标不存在（等价于 GetTheme 之后被并发删除）→ 第二步匹配 0 行。
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
