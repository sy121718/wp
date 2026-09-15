package feature

// theme_slot_test.go — 主题级结构槽位（审计 VIS-012）。
//
// 验收：**配置一次**全站公告条，所有页面都出现。
// 这条能力此前不存在 —— settings.structure 只有页眉/页脚两个槽，其余 kind 的块
// （公告条 / 侧边栏 / 抽屉）只能靠作者在每个页面手动插一份 globalref，
// 结果是「加了公告条要改 N 个页面」，而且总会漏几个。
//
// 用例刻意建两个页面：只建一个页面时，「全站生效」与「碰巧生成了」看起来一样。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

const announcementBlockDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"ann-mark","props":{"text":"SITE-ANNOUNCEMENT"}}]}`

// TestThemeAnnouncementSlotAppliesToAllPages 主题配置公告条槽位后，全站页面都出现。
func TestThemeAnnouncementSlotAppliesToAllPages(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	// blocks 表由生产迁移建（project_id uuid → projects 外键、document jsonb、id identity），
	// 手抄 DDL 曾把 project_id 写成 TEXT、document 写成 JSON，与生产分叉。
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)

	announcement, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "全站公告条", Kind: "announcement",
		Document: json.RawMessage(announcementBlockDocument),
	})
	if err != nil {
		t.Fatalf("创建公告条块失败: %v", err)
	}

	// 主题里**只配一次**：slots.announcement → 该块。
	// 页眉页脚的既有字段保持不变，两种写法在 SlotBindings() 里合并。
	themeSettings, _ := json.Marshal(map[string]any{
		"colors": map[string]any{"primary": "#2563eb"},
		"slots":  map[string]string{"announcement": announcement.ID},
	})
	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "默认主题", Settings: themeSettings,
	})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}

	// 两个页面都挂这个主题（第一个主题自动激活，页面保存时快照结构绑定）。
	// 两个页面必须用不同的 kind：无内容目标的 kind（home / archive / search / notFound）
	// 才能带 targetType=none，而 page/article/tag 必须挂内容目标。
	pages := []struct{ path, kind string }{
		{"/slot-a", "home"},
		{"/slot-b", "search"},
	}
	for _, pc := range pages {
		p := pc.path
		page, cerr := svc.Create(ctx, &pagedto.CreateReq{
			ProjectID: projectID, Kind: pc.kind, ContentTargetType: "none",
			DraftPath: p, DraftDocument: json.RawMessage(pageDocument),
		})
		if cerr != nil {
			t.Fatalf("创建页面 %s 失败: %v", p, cerr)
		}
		if page.ThemeID != theme.ID {
			t.Fatalf("页面 %s 应挂激活主题: %+v", p, page.ThemeID)
		}
		detail, derr := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
		if derr != nil {
			t.Fatalf("查询页面 %s 失败: %v", p, derr)
		}
		var doc struct {
			Settings struct {
				Structure struct {
					Slots map[string]string `json:"slots"`
				} `json:"structure"`
			} `json:"settings"`
		}
		if uerr := json.Unmarshal(detail.DraftDocument, &doc); uerr != nil {
			t.Fatalf("解析页面 %s 文档失败: %v", p, uerr)
		}
		if doc.Settings.Structure.Slots["announcement"] != announcement.ID {
			t.Fatalf("页面 %s 应快照公告条槽位绑定，实际 %+v", p, doc.Settings.Structure.Slots)
		}

		built, berr := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
		if berr != nil {
			t.Fatalf("构建页面 %s 失败: %v", p, berr)
		}
		html, rerr := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", built.StagedHash, "index.html"))
		if rerr != nil {
			t.Fatalf("读取 %s 产物失败: %v", p, rerr)
		}
		if !containsBytes(html, []byte("SITE-ANNOUNCEMENT")) {
			t.Fatalf("页面 %s 的产物应包含公告条内容（主题配一次，全站生效）", p)
		}
	}
}
