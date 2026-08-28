package feature

// theme_crud_test.go — 主题链路 feature：新建(首个自动激活)/防重/激活切换/更新/删除守卫。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newProjectThemeService(t *testing.T) *projectservice.Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, settings JSON NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE TABLE themes (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, name TEXT NOT NULL, settings JSON NOT NULL, is_active BOOLEAN NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
	return projectservice.NewService(projectmodel.NewProjectModel(db))
}

func TestThemeLifecycleActivateAndUpdate(t *testing.T) {
	svc := newProjectThemeService(t)
	ctx := context.Background()

	project, err := svc.Create(ctx, &projectdto.CreateReq{Name: "官网"})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}
	// 首个主题自动激活。
	first, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "默认主题"})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	if !first.IsActive {
		t.Fatalf("首个主题应自动激活: %+v", first)
	}
	// 第二个主题不激活；激活列表跟随。
	second, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "春季版"})
	if err != nil {
		t.Fatalf("创建第二主题失败: %v", err)
	}
	if second.IsActive {
		t.Fatalf("非首个主题不应自动激活: %+v", second)
	}
	if err = svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: second.ID}); err != nil {
		t.Fatalf("切换激活失败: %v", err)
	}
	active, err := svc.GetActiveTheme(ctx, project.ID)
	if err != nil || active.ID != second.ID {
		t.Fatalf("激活主题应切换为第二主题: %+v err=%v", active, err)
	}
	// 更新设置与 GetTheme 回读。
	settings, _ := json.Marshal(map[string]any{
		"colors":       map[string]string{"primary": "#2563eb"},
		"fontFamily":   "system-ui, sans-serif",
		"headerPageId": "p-header",
	})
	updated, err := svc.UpdateTheme(ctx, &projectdto.ThemeUpdateReq{ID: first.ID, Settings: settings})
	if err != nil {
		t.Fatalf("更新主题失败: %v", err)
	}
	got, err := svc.GetTheme(ctx, first.ID)
	if err != nil || !strings.Contains(string(got.Settings), "#2563eb") {
		t.Fatalf("设置未持久化: %+v err=%v", got, err)
	}
	if updated.Name != first.Name {
		t.Fatalf("未传名称时不应改名: %+v", updated)
	}
	themes, err := svc.ListThemes(ctx, project.ID)
	if err != nil || len(themes) != 2 || themes[0].ID != second.ID {
		t.Fatalf("主题列表应激活在前: %+v err=%v", themes, err)
	}
}

func TestThemeRejectsDuplicateName(t *testing.T) {
	svc := newProjectThemeService(t)
	ctx := context.Background()
	project, _ := svc.Create(ctx, &projectdto.CreateReq{Name: "官网"})
	if _, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "默认主题"}); err != nil {
		t.Fatalf("首个主题创建失败: %v", err)
	}
	if _, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "  默认主题 "}); err == nil {
		t.Fatalf("同名主题应被拒绝")
	}
}

func TestThemeDeleteGuardsActive(t *testing.T) {
	svc := newProjectThemeService(t)
	ctx := context.Background()
	project, _ := svc.Create(ctx, &projectdto.CreateReq{Name: "官网"})
	first, _ := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "默认主题"})
	second, _ := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "春季版"})

	// 激活态删除应被拒绝。
	if err := svc.DeleteTheme(ctx, first.ID); err == nil {
		t.Fatalf("激活主题删除应被拒绝")
	}
	// 切换后可删除。
	if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: second.ID}); err != nil {
		t.Fatalf("切换激活失败: %v", err)
	}
	if err := svc.DeleteTheme(ctx, first.ID); err != nil {
		t.Fatalf("非激活主题应可删除: %v", err)
	}
	themes, _ := svc.ListThemes(ctx, project.ID)
	if len(themes) != 1 {
		t.Fatalf("删除后应只剩一个主题: %+v", themes)
	}
}
