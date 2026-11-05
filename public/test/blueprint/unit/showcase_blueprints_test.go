package unit

import (
	"context"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
	"gorm.io/gorm"

	"go_wp/pkg/builder"
	blueprintmodel "go_wp/internal/module/blueprint/model"
	blueprintservice "go_wp/internal/module/blueprint/service"
	"go_wp/internal/templates"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

var showcaseBlueprintIDs = []string{
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c301",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c302",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c303",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c304",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c305",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c306",
}

type showcaseExpectation struct {
	BlueprintID string
	Markers     []string
}

var showcaseExpectations = []showcaseExpectation{
	{"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c301", []string{
		"home-hero-title", "<h1", "home-features-grid", "home-cta-btn",
	}},
	{"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c302", []string{
		"shop-grid", "sky-card", "shop-item-6",
	}},
	{"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c303", []string{
		"about-story-grid", "about-values-grid", "about-cta-btn",
	}},
	{"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c304", []string{
		"contact-form-node", "<form", "type=\"email\"", "contact-channel-grid",
	}},
	{"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c305", []string{
		"policy-toc-list", "policy-s1", "policy-s6-p",
	}},
	{"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c306", []string{
		"faq-general-list", "<details", "<summary", "faq-billing-list",
	}},
}

func showcaseComponentSet(t *testing.T) *jet.Set {
	t.Helper()
	set, err := templates.NewComponentSet("../../../../internal/templates/components")
	if err != nil {
		t.Fatalf("加载组件模板 Set 失败: %v", err)
	}
	return set
}

func seedShowcaseBlueprints(t *testing.T) *gorm.DB {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}
	return db
}

func loadShowcaseBlueprints(t *testing.T, db *gorm.DB) []*blueprintmodel.BlueprintEntity {
	t.Helper()
	rows := make([]*blueprintmodel.BlueprintEntity, 0, len(showcaseBlueprintIDs))
	if err := db.Where("id IN ?", showcaseBlueprintIDs).Find(&rows).Error; err != nil {
		t.Fatalf("查询预置蓝图失败: %v", err)
	}
	if len(rows) != len(showcaseBlueprintIDs) {
		t.Fatalf("预置展示页蓝图数量应为 %d，实际 %d（迁移 287 未执行或 seed 被跳过）",
			len(showcaseBlueprintIDs), len(rows))
	}
	return rows
}

func TestShowcaseBlueprintsSeeded(t *testing.T) {
	db := seedShowcaseBlueprints(t)
	if db == nil {
		return
	}
	for _, row := range loadShowcaseBlueprints(t, db) {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			if !blueprintmodel.IsValidKind(row.Kind) {
				t.Fatalf("kind %q 不在 PageKinds 白名单内", row.Kind)
			}
			page, err := builder.ParsePage(row.DraftDocument)
			if err != nil {
				t.Fatalf("DraftDocument 解析失败: %v", err)
			}
			if err := builder.ValidatePage(page); err != nil {
				t.Fatalf("DraftDocument 未通过页面校验: %v", err)
			}
			if len(page.Root) == 0 {
				t.Fatalf("DraftDocument 的 root 为空（蓝图没有内容）")
			}
			var versions int64
			if err := db.Model(&blueprintmodel.VersionEntity{}).
				Where("blueprint_id = ? AND version = ?", row.ID, row.DraftVersion).
				Count(&versions).Error; err != nil {
				t.Fatalf("查询蓝图版本失败: %v", err)
			}
			if versions != 1 {
				t.Fatalf("蓝图 %s 缺少 version=%d 的版本行（InitPageDocument 会失败）", row.ID, row.DraftVersion)
			}
		})
	}
}

func TestShowcaseBlueprintsCompile(t *testing.T) {
	db := seedShowcaseBlueprints(t)
	if db == nil {
		return
	}
	set := showcaseComponentSet(t)
	rows := loadShowcaseBlueprints(t, db)

	expectByID := make(map[string][]string, len(showcaseExpectations))
	for _, exp := range showcaseExpectations {
		expectByID[exp.BlueprintID] = exp.Markers
	}

	for _, row := range rows {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			page, err := builder.ParsePage(row.DraftDocument)
			if err != nil {
				t.Fatalf("DraftDocument 解析失败: %v", err)
			}
			compiled, err := builder.Compile(page, builder.WithComponentSet(set))
			if err != nil {
				t.Fatalf("编译失败: %v", err)
			}
			if strings.TrimSpace(compiled.HTML) == "" {
				t.Fatalf("编译产物 HTML 为空")
			}
			if strings.TrimSpace(compiled.CSS) == "" {
				t.Fatalf("编译产物 CSS 为空")
			}

			doc, err := builder.RenderDocument(compiled)
			if err != nil {
				t.Fatalf("组装文档失败: %v", err)
			}
			if strings.TrimSpace(doc) == "" {
				t.Fatalf("组装后的整页文档为空")
			}
			if !strings.Contains(compiled.HTML, "sky-c-") {
				t.Fatalf("产物没有任何组件作用域 class（组件未参与渲染）")
			}
			for _, marker := range expectByID[row.ID] {
				if !strings.Contains(compiled.HTML, marker) {
					t.Fatalf("产物缺少该页面语义所必需的标记 %q，产物开头：%s", marker, firstN(compiled.HTML, 400))
				}
			}
		})
	}
}

func TestShowcaseBlueprintsSeedIdempotent(t *testing.T) {
	db := seedShowcaseBlueprints(t)
	if db == nil {
		return
	}

	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("重复执行种子数据失败（幂等性被破坏）: %v", err)
	}
	var blueprints, versions int64
	if err := db.Model(&blueprintmodel.BlueprintEntity{}).
		Where("id IN ?", showcaseBlueprintIDs).Count(&blueprints).Error; err != nil {
		t.Fatalf("统计蓝图失败: %v", err)
	}
	if err := db.Model(&blueprintmodel.VersionEntity{}).
		Where("blueprint_id IN ?", showcaseBlueprintIDs).Count(&versions).Error; err != nil {
		t.Fatalf("统计蓝图版本失败: %v", err)
	}
	if blueprints != int64(len(showcaseBlueprintIDs)) || versions != int64(len(showcaseBlueprintIDs)) {
		t.Fatalf("重复执行后行数应各为 %d，实际 蓝图=%d 版本=%d",
			len(showcaseBlueprintIDs), blueprints, versions)
	}
}

func newShowcasePageService(t *testing.T, db *gorm.DB) (pagecontract.PageService, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "展示页蓝图测试站点"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	pageModel := pagemodel.NewPageModel(db)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	plugins := pluginservice.NewService(pluginmodel.NewModel(db))
	svc := pageservice.NewService(pageModel, artifacts, routes, projects, blocks, plugins, nil, nil, nil)
	return svc, project.ID
}

func TestShowcaseBlueprintsCloneAndPreview(t *testing.T) {
	db := seedShowcaseBlueprints(t)
	if db == nil {
		return
	}
	pageSvc, projectID := newShowcasePageService(t, db)
	bpSvc := blueprintservice.NewService(blueprintmodel.NewModel(db))
	ctx := context.Background()

	originalIDs := []string{
		"home-hero-title", "home-features-grid", "shop-grid", "about-story-grid",
		"contact-form-node", "policy-toc-list", "faq-general-list",
	}
	for _, row := range loadShowcaseBlueprints(t, db) {
		t.Run(row.Name, func(t *testing.T) {
			cloned, err := bpSvc.InitPageDocument(ctx, row.ID)
			if err != nil {
				t.Fatalf("InitPageDocument 失败: %v", err)
			}
			for _, marker := range originalIDs {
				if strings.Contains(string(cloned), marker) {
					t.Fatalf("复制后仍保留蓝图原始节点 ID %q（应递归重写，否则两页共用 sky-c-<id> 类互相污染）", marker)
				}
			}
			page, err := builder.ParsePage(cloned)
			if err != nil {
				t.Fatalf("复制后的文档解析失败: %v", err)
			}
			if err := builder.ValidatePage(page); err != nil {
				t.Fatalf("复制后的文档未通过页面校验: %v", err)
			}
			html, err := pageSvc.CompilePreview(ctx, cloned, projectID, "/", "", false)
			if err != nil {
				t.Fatalf("预览编译失败: %v", err)
			}
			body := string(html)
			if len(strings.TrimSpace(body)) == 0 {
				t.Fatalf("预览编译产物为空")
			}

			if title := strings.TrimSpace(page.Settings.SEO.Title); title != "" && !strings.Contains(body, title) {
				t.Fatalf("预览产物缺少页面标题 %q", title)
			}
			if !strings.Contains(body, "sky-c-") {
				t.Fatalf("预览产物没有任何组件作用域 class")
			}
		})
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
