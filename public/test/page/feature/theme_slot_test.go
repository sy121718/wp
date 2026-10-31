package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
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

func TestThemeAnnouncementSlotAppliesToAllPages(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)

	announcement, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "全站公告条", Kind: "announcement",
		Document: json.RawMessage(announcementBlockDocument),
	})
	if err != nil {
		t.Fatalf("创建公告条块失败: %v", err)
	}

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
