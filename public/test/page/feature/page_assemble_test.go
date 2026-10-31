package feature

import (
	"context"
	"fmt"
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

const headerBlockDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"hdr-mark","props":{"text":"GLOBAL-HEADER-MARK"}}]}`

func TestPageAssembleInlinesHeaderBlock(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)

	header, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "站点页眉", Kind: "header",
		Document: json.RawMessage(headerBlockDocument),
	})
	if err != nil {
		t.Fatalf("创建页眉块失败: %v", err)
	}
	themeSettings, _ := json.Marshal(map[string]any{"headerBlockId": header.ID})
	theme, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "默认主题", Settings: themeSettings,
	})
	if err != nil {
		t.Fatalf("创建主题失败: %v", err)
	}

	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/assembled", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	if page.ThemeID != theme.ID {
		t.Fatalf("页面应挂激活主题: %+v", page)
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	var doc struct {
		Settings struct {
			Structure struct {
				HeaderBlockID string `json:"headerBlockId"`
			} `json:"structure"`
		} `json:"settings"`
	}
	if err = json.Unmarshal(detail.DraftDocument, &doc); err != nil {
		t.Fatalf("草稿文档解析失败: %v", err)
	}
	if doc.Settings.Structure.HeaderBlockID != header.ID {
		t.Fatalf("settings.structure 应快照页眉块绑定: %+v", doc.Settings.Structure)
	}

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", built.StagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if !containsBytes(html, []byte("GLOBAL-HEADER-MARK")) {
		t.Fatalf("产物应内联页眉块内容: %s", string(html[:min(len(html), 400)]))
	}

	if dir := os.Getenv("GO_WP_DUMP_ARTIFACT"); dir != "" {
		if werr := os.WriteFile(filepath.Join(dir, "assembled.html"), html, 0o644); werr != nil {
			t.Fatalf("dump 产物失败: %v", werr)
		}
	}

	if _, err = svc.MarkStaleForTheme(ctx, theme.ID); err != nil {
		t.Fatalf("标记 stale 失败: %v", err)
	}
	staled, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: page.ID})
	if err != nil || !staled.Stale {
		t.Fatalf("页眉变更后页面应标 stale: %+v err=%v", staled, err)
	}
}

func containsBytes(haystack, needle []byte) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle []byte) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestGlobalRefNodeInlinesBlockContent(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projectservice.NewService(projectmodel.NewProjectModel(db)))
	block, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: projectID, Name: "促销横幅", Kind: "block",
		Document: json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"promo1","props":{"text":"PROMO-REF-MARK"}}]}`),
	})
	if err != nil {
		t.Fatalf("创建内容块失败: %v", err)
	}

	doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.globalref","id":"ref1","props":{"blockId":"%s"}}]}`, block.ID)
	page, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/with-ref", DraftDocument: json.RawMessage(doc),
	})
	if err != nil {
		t.Fatalf("创建含引用节点页面失败: %v", err)
	}
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(os.Getenv("GO_WP_ARTIFACT_ROOT"), "artifacts", built.StagedHash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if !containsBytes(html, []byte("PROMO-REF-MARK")) {
		t.Fatalf("产物应内联引用块内容: %s", string(html[:min(len(html), 400)]))
	}
}
