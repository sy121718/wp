package feature

// page_theme_test.go — 页面挂接主题链路：创建自动挂激活主题、
// 首个主题创建后回填未挂页面、List 按主题过滤。

import (
	"context"
	"encoding/json"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

func TestPageThemeBindingAndFilter(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))

	// 无主题阶段：创建页面不挂主题（ThemeID 空），List 全量可见。
	first, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/first", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	if first.ThemeID != "" {
		t.Fatalf("无主题时页面不应挂主题: %+v", first)
	}

	// 工程首个主题自动激活；创建后回填未挂主题的历史页面。
	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{ProjectID: projectID, Name: "默认主题"})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}
	if err = svc.AttachThemeToUnassigned(ctx, projectID, theme.ID); err != nil {
		t.Fatalf("回填未挂页面失败: %v", err)
	}
	bound, err := svc.Detail(ctx, &pagedto.DetailReq{ID: first.ID})
	if err != nil || bound.ThemeID != theme.ID {
		t.Fatalf("历史页面应回填到新主题: %+v err=%v", bound, err)
	}

	// 回填后创建的页面自动挂激活主题；按主题过滤可见、按他主题过滤不可见。
	second, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/second", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建第二页面失败: %v", err)
	}
	if second.ThemeID != theme.ID {
		t.Fatalf("新页面应自动挂激活主题: %+v", second)
	}
	byTheme, err := svc.List(ctx, theme.ID)
	if err != nil || len(byTheme) != 2 {
		t.Fatalf("按激活主题过滤应见两页: n=%d err=%v", len(byTheme), err)
	}
	byEmpty, err := svc.List(ctx, "")
	if err != nil || len(byEmpty) != 2 {
		t.Fatalf("空主题应列全部页面: n=%d err=%v", len(byEmpty), err)
	}
	byOther, err := svc.List(ctx, "00000000-0000-0000-0000-000000000000")
	if err != nil || len(byOther) != 0 {
		t.Fatalf("其他主题过滤应为空: n=%d err=%v", len(byOther), err)
	}
}
