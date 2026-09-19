// Package unit — 预置「展示页」Blueprint 的 feature 测试（真实 PostgreSQL）。
//
// 覆盖迁移 287 的六个展示页蓝图：断言它们确实落库、DraftDocument 能解析且 kind 合法
// （在 pageenums.PageKinds 白名单内），并且**能经构建器编译出非空 HTML**。
//
// 为什么必须真编译：Seed 里的文档是大段 JSON 字面量，改一个字段名、写错一个组件类型，
// 肉眼读不出来，而失败点会推迟到用户「新建页面 → 预览」时才暴露。本用例把这一步
// 前移到测试：组件注册表与声明式校验（core.ValidateNode → ValidateSpec）都会在此跑一遍。
package unit

import (
	"context"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
	"gorm.io/gorm"

	"go_wp/internal/builder"
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

// showcaseBlueprintIDs 迁移 287 seed 的六个蓝图（固定 UUID 字面量，幂等键）。
var showcaseBlueprintIDs = []string{
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c301",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c302",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c303",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c304",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c305",
	"3f1b6a52-7c4d-4a01-9e10-11f0a6b2c306",
}

// showcaseExpectation 单个蓝图的语义特征：编译产物里必须出现的标记。
// 断言的是「这个页面确实表达了它的语义」，而不是「JSON 里有这些字段」。
type showcaseExpectation struct {
	BlueprintID string
	Markers     []string
}

// showcaseExpectations 六个页面各自的产物标记：
// 首页 hero + 特色区块 + CTA；商店商品卡组；关于页故事/价值观；联系页表单；
// 政策页目录与分条正文；FAQ 页原生 details/summary 折叠（含分组标题）。
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

// showcaseComponentSet 组件模板 Set（测试进程工作目录为 public/test/blueprint/unit）。
func showcaseComponentSet(t *testing.T) *jet.Set {
	t.Helper()
	set, err := templates.NewComponentSet("../../../../internal/templates/components")
	if err != nil {
		t.Fatalf("加载组件模板 Set 失败: %v", err)
	}
	return set
}

// seedShowcaseBlueprints 建隔离 schema（跑生产迁移）并执行全部种子数据。
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

// loadShowcaseBlueprints 取回六个预置蓝图，数量不足直接失败。
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

// TestShowcaseBlueprintsSeeded 六个展示页蓝图已 seed，DraftDocument 可解析、kind 合法、
// 版本行存在（InitPageDocument 取的是版本行的 document）。
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

// TestShowcaseBlueprintsCompile 六个蓝图的文档都能编译成非空 HTML/CSS，
// 且产物里出现该页面语义所必需的标记。
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
			// 整页文档（含 head 与内联 CSS）也必须能组装出来且非空。
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

// TestShowcaseBlueprintsSeedIdempotent 重复执行种子不报错、不产生重复行。
func TestShowcaseBlueprintsSeedIdempotent(t *testing.T) {
	db := seedShowcaseBlueprints(t)
	if db == nil {
		return
	}
	// 第二次执行：条件判定应命中「已完成」而跳过，行数保持不变。
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

// newShowcasePageService 装配 page service 的真实依赖（同一隔离 schema），并建一个真实工程
// （建站自带默认主题，与生产一致）。返回服务与工程 ID。
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

// TestShowcaseBlueprintsCloneAndPreview 走真实链路：InitPageDocument 复制 AST（重写节点 ID）
// → 预览编译（与正式构建同源的 compileDocument 装配）→ 产物非空且保留页面语义。
//
// 为什么这条用例不能省：只测 builder.Compile 时注入的是测试用的组件模板目录，而
// 生产预览走的是 embed 组件集 + 站点装配（主题 / 槽位 / 客户端资产）。蓝图最终要落进
// 这条链路，只有它跑通才算「新建页面即可用」。
func TestShowcaseBlueprintsCloneAndPreview(t *testing.T) {
	db := seedShowcaseBlueprints(t)
	if db == nil {
		return
	}
	pageSvc, projectID := newShowcasePageService(t, db)
	bpSvc := blueprintservice.NewService(blueprintmodel.NewModel(db))
	ctx := context.Background()

	// 蓝图里的示例节点 ID 前缀：复制后一个都不该留下。
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
			html, err := pageSvc.CompilePreview(ctx, cloned, projectID, "/", "")
			if err != nil {
				t.Fatalf("预览编译失败: %v", err)
			}
			body := string(html)
			if len(strings.TrimSpace(body)) == 0 {
				t.Fatalf("预览编译产物为空")
			}
			// 预览是完整文档（含 <head>）：标题来自蓝图的 settings.seo，组件作用域类
			// 来自本次编译 —— 两者都在，说明预览确实渲染了这份文档而不是空壳。
			if title := strings.TrimSpace(page.Settings.SEO.Title); title != "" && !strings.Contains(body, title) {
				t.Fatalf("预览产物缺少页面标题 %q", title)
			}
			if !strings.Contains(body, "sky-c-") {
				t.Fatalf("预览产物没有任何组件作用域 class")
			}
		})
	}
}

// firstN 返回字符串前 n 字节（错误信息里只给产物开头，避免刷屏）。
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
