package feature

// theme_crud_test.go — 主题链路 feature：新建(首个自动激活)/防重/激活切换/更新/删除守卫。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/builder"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

func newProjectThemeService(t *testing.T) *projectservice.Service {
	t.Helper()
	// 表结构走生产迁移（projects / themes 都是真实 DDL），不再手抄：手抄版本
	// 用 TEXT 主键，与生产的 uuid 脱节，改列类型时会静默失配。
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
	// 建站即有主题：工程创建时自动建一套默认主题并激活（继承链「主题 → 页面 → 组件」
	// 的起点必须先存在，否则页面拿不到令牌、组件只能落到内置 fallback）。
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
	// 再建的主题不再自动激活（默认主题已经占住激活位）。
	first, err := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "默认主题"})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	if first.IsActive {
		t.Fatalf("已有激活主题时新建主题不应自动激活: %+v", first)
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
	// 三个主题：建站自动带的默认主题 + 上面手动建的两个；激活的排在最前。
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
	// 建站自带的默认主题就是当前的激活态。
	seeded, err := svc.GetActiveTheme(ctx, project.ID)
	if err != nil || seeded == nil {
		t.Fatalf("新工程应有激活主题: %v", err)
	}
	second, _ := svc.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: project.ID, Name: "春季版"})

	// 激活态删除应被拒绝。
	if err := svc.DeleteTheme(ctx, seeded.ID); err == nil {
		t.Fatalf("激活主题删除应被拒绝")
	}
	// 切换后可删除。
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

// TestActivateThemeMissingTargetKeepsOtherActive 激活目标不存在时必须报错并回滚。
//
// 事务第一步会把同工程其余主题全部取消激活；若第二步 UPDATE 匹配 0 行还不报错
// （目标在 GetTheme 之后被并发删除），事务照样提交 —— 整个工程 is_active 全 false，
// 而 API 回报「激活成功」。这里断言失败后仍恰好有一套激活主题，且原主题没被抹掉。
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
	// 建站自带的默认主题是原激活态（回滚后它必须还在激活位上）。
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
