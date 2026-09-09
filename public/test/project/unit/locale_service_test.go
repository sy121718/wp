package unit

// locale_service_test.go — 站点语言清单（多语言 P3，docs/06-D §14 D10）。

import (
	"context"
	"testing"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/test/support"
)

// newLocaleService 隔离 PG schema + AutoMigrate projects/project_locales。
func newLocaleService(t *testing.T) (*projectservice.Service, string) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil, ""
	}
	if err := db.AutoMigrate(&projectmodel.ProjectEntity{}, &projectmodel.LocaleEntity{}); err != nil {
		t.Fatalf("AutoMigrate projects/project_locales 失败: %v", err)
	}
	svc := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := svc.Create(context.Background(), &projectdto.CreateReq{Name: "语言清单测试工程"})
	if err != nil {
		t.Fatalf("创建工程失败: %v", err)
	}
	return svc, project.ID
}

func boolPtr(b bool) *bool { return &b }

// TestProjectLocalesSaveAndList 保存清单：默认语言在前，启用语言顺序稳定。
func TestProjectLocalesSaveAndList(t *testing.T) {
	svc, projectID := newLocaleService(t)
	ctx := context.Background()

	saved, err := svc.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "en-US", SortOrder: 1},
			{Lang: "zh-CN", IsDefault: true},
			{Lang: "ja-JP", Enabled: boolPtr(false)},
		},
	})
	if err != nil {
		t.Fatalf("保存语言清单失败: %v", err)
	}
	if len(saved) != 3 || saved[0].Lang != "zh-CN" || !saved[0].IsDefault {
		t.Fatalf("默认语言应排在最前: %+v", saved)
	}
	langs, err := svc.EnabledLangs(ctx, projectID)
	if err != nil {
		t.Fatalf("读取启用语言失败: %v", err)
	}
	// 默认语言在前，禁用语言不出现。
	if len(langs) != 2 || langs[0] != "zh-CN" || langs[1] != "en-US" {
		t.Fatalf("启用语言错误: %+v", langs)
	}
	def, err := svc.DefaultLocale(ctx, projectID)
	if err != nil || def != "zh-CN" {
		t.Fatalf("默认语言错误: %q err=%v", def, err)
	}

	// 全量替换幂等：重复保存同样输入结果一致。
	again, err := svc.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "en-US", SortOrder: 1},
			{Lang: "zh-CN", IsDefault: true},
			{Lang: "ja-JP", Enabled: boolPtr(false)},
		},
	})
	if err != nil {
		t.Fatalf("重复保存失败: %v", err)
	}
	if len(again) != len(saved) {
		t.Fatalf("重复保存应全量替换: %+v vs %+v", again, saved)
	}
}

// TestProjectLocalesFallback 无清单记录时回退站点默认语言一种（单语言兼容）。
func TestProjectLocalesFallback(t *testing.T) {
	svc, projectID := newLocaleService(t)
	ctx := context.Background()

	langs, err := svc.EnabledLangs(ctx, projectID)
	if err != nil {
		t.Fatalf("读取启用语言失败: %v", err)
	}
	if len(langs) != 1 || langs[0] == "" {
		t.Fatalf("无清单应回退一种默认语言: %+v", langs)
	}
	def, err := svc.DefaultLocale(ctx, projectID)
	if err != nil || def != langs[0] {
		t.Fatalf("默认语言应与回退一致: %q vs %+v err=%v", def, langs, err)
	}
}

// TestProjectLocalesValidation 清单校验：空清单/重复/双默认/默认禁用/非法语言码。
func TestProjectLocalesValidation(t *testing.T) {
	svc, projectID := newLocaleService(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		items []projectdto.LocaleItem
	}{
		{"空清单", nil},
		{"重复语言", []projectdto.LocaleItem{{Lang: "zh-CN"}, {Lang: "zh-CN"}}},
		{"两个默认", []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true}, {Lang: "en-US", IsDefault: true}}},
		{"默认禁用", []projectdto.LocaleItem{{Lang: "zh-CN", IsDefault: true, Enabled: boolPtr(false)}}},
		{"非法语言码", []projectdto.LocaleItem{{Lang: "zh CN"}}},
		{"超长语言码", []projectdto.LocaleItem{{Lang: "zh-CN-abcdefghijklmnopqrstuvwxyz-0123456789"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.SaveLocales(ctx, &projectdto.LocalesSaveReq{ProjectID: projectID, Locales: c.items}); err == nil {
				t.Fatalf("非法清单应被拒绝: %+v", c.items)
			}
		})
	}
}
