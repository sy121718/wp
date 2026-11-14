package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/builder"

	"go_wp/public/test/support"
)

func newProjectThemeService(t *testing.T) *projectservice.Service {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)
	return projectservice.NewService(projectmodel.NewProjectModel(db))
}

func TestThemeLifecycleActivateAndUpdate(t *testing.T) {
	svc := newProjectThemeService(t)
	ctx := context.Background()

	project, err := svc.Create(ctx, &projectdto.CreateReq{Name: "官网"})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}

	seeded, err := svc.GetActiveTheme(ctx, project.ID)
	if err != nil {
		t.Fatalf("新工程应已有激活主题: %v", err)
	}
	if seeded.Name != builder.DefaultThemeName {
		t.Fatalf("默认主题名称不符: %+v", seeded)
	}
	if seeded.Settings == nil || len(seeded.Settings) == 0 {
		t.Fatalf("默认主题应带后台风格色值: %+v", seeded)
	}

	first, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "默认主题"})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	if first.IsActive {
		t.Fatalf("已有激活主题时新建主题不应自动激活: %+v", first)
	}

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
	if err != nil || len(themes) != 3 || themes[0].ID != second.ID {
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

	seeded, err := svc.GetActiveTheme(ctx, project.ID)
	if err != nil || seeded == nil {
		t.Fatalf("新工程应有激活主题: %v", err)
	}
	second, _ := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "春季版"})

	if err := svc.DeleteTheme(ctx, seeded.ID); err == nil {
		t.Fatalf("激活主题删除应被拒绝")
	}

	if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: second.ID}); err != nil {
		t.Fatalf("切换激活失败: %v", err)
	}
	if err := svc.DeleteTheme(ctx, seeded.ID); err != nil {
		t.Fatalf("非激活主题应可删除: %v", err)
	}
	themes, _ := svc.ListThemes(ctx, project.ID)
	if len(themes) != 1 {
		t.Fatalf("删除后应只剩手动建的那一个主题: %+v", themes)
	}
}

func TestActivateThemeMissingTargetKeepsOtherActive(t *testing.T) {
	svc := newProjectThemeService(t)
	ctx := context.Background()

	project, err := svc.Create(ctx, &projectdto.CreateReq{Name: "站点"})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}
	first, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "主题A"})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}

	seeded, err := svc.GetActiveTheme(ctx, project.ID)
	if err != nil || seeded == nil {
		t.Fatalf("新工程应有激活主题: %v", err)
	}

	const missing = "00000000-0000-0000-0000-000000000000"
	if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: missing}); err == nil {
		t.Errorf("激活不存在的主题必须返回错误")
	}

	themes, err := svc.ListThemes(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListThemes: %v", err)
	}
	active := 0
	for _, th := range themes {
		if th.IsActive {
			active++
		}
	}
	if active != 1 {
		t.Errorf("激活失败后仍应恰好有一套激活主题，got %d：%+v", active, themes)
	}
	for _, th := range themes {
		if th.ID == seeded.ID && !th.IsActive {
			t.Errorf("原激活主题被事务第一步抹掉了：%+v", th)
		}
		if th.ID == first.ID && th.IsActive {
			t.Errorf("激活失败不应把激活位给了别的主题：%+v", th)
		}
	}
}
