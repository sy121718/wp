package feature

// theme_activate_reskin_test.go — 修复遗留缺陷 H5（审计 High）：
// 切换激活主题后「整站换皮」实际不生效。
//
// 复现路径：工程下页面挂接激活主题 A，切换激活主题到 B 后，页面仍渲染 A 的
// settings.theme 快照 → 换皮不生效。
// 本次断言：ActivateTheme（dashboard handler /admin/themes/activate）成功后，
// 该工程整站页面转挂到 B（theme_id=B），settings.theme/structure 刷新为 B 的设置，
// 且全部页面标记待重建（stale=true）。
//
// 由 dashboard 主题管理 handler 触发，走真实 PostgreSQL schema + 真实服务装配。
// 建库走生产迁移（support.NewMigratedPGTestDB）：themes/pages 的 settings、
// draft_document 在生产即是 jsonb，jsonb_set 快照刷新天然可用。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projecthttp "go_wp/internal/module/project/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// makeJsonbPageService 用**生产迁移**建库并装配 page/project/block 服务，
// 返回 db、pages 契约、projects 服务与工程 ID。
// （名字保留：生产 schema 的 draft_document / settings 本即 jsonb，jsonb_set 快照刷新可用。）
func makeJsonbPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	// 建站会自带一套默认主题（生产语义：先有主题再有页面）。本用例要自己控制
	// 「谁是首个主题、谁被激活」，所以先清掉默认主题，前提干净。
	if err := db.Exec(`DELETE FROM themes WHERE project_id = ?`, project.ID).Error; err != nil {
		t.Fatalf("清理默认主题失败: %v", err)
	}
	pageModel := pagemodel.NewPageModel(db)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	svc := pageservice.NewService(pageModel, artifacts, routes, projects, blocks, nil, nil, nil, nil)
	return db, svc, projects, project.ID
}

// TestActivateThemeReskinsWholeSite 激活新主题后整站页面应刷新为它的外观：
// theme_id 转挂、settings.theme/structure 更新为 B、全部标记待重建。
func TestActivateThemeReskinsWholeSite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projects, projectID := makeJsonbPageService(t)
	ctx := context.Background()

	// 主题 A（首个自动激活）+ 主题 B（含不同颜色/字体/页眉页脚绑定）。
	themeA, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "默认亮色",
		Settings: json.RawMessage(`{"colors":{"primary":"#111111"},"typography":{"body":{"fontFamily":"Arial"}}}`),
	})
	if err != nil {
		t.Fatalf("创建主题 A 失败: %v", err)
	}
	if !themeA.IsActive {
		t.Fatalf("首个主题应自动激活: %+v", themeA)
	}
	themeB, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "春季暗色",
		Settings: json.RawMessage(`{"colors":{"primary":"#2563eb"},"typography":{"body":{"fontFamily":"Serif"}},"headerBlockId":"new-hdr","footerBlockId":"new-ftr"}`),
	})
	if err != nil {
		t.Fatalf("创建主题 B 失败: %v", err)
	}
	if themeB.IsActive {
		t.Fatalf("第二个主题不应自动激活: %+v", themeB)
	}

	// 激活主题 A 下建两页：自动挂 A，快照为 A 的偏色。
	pageA := createJsonbPage(t, svc, projectID, "/reskin-a", reskinDoc)
	pageB := createJsonbPage(t, svc, projectID, "/reskin-b", reskinDoc)
	for _, p := range []*pagedto.PageResp{pageA, pageB} {
		if p.ThemeID != themeA.ID {
			t.Fatalf("页面应挂接主题 A: %+v", p)
		}
		assertThemeSnapshotX(t, p.DraftDocument, "#111111", "Arial")
		assertStructureSnapshotX(t, p.DraftDocument, "", "")
		// 建页后默认 stale=true；先清零以验证激活后重新标脏。
		db.Table("pages").Where("id = ?", p.ID).Update("stale", false)
	}

	// 通过 dashboard handler 激活主题 B（POST /admin/themes/activate），
	// 走真实 ActivateTheme → reskinProjectPages 编排。
	if code := activateThemeHTTP(t, svc, projects, themeB.ID); code != http.StatusSeeOther {
		t.Fatalf("激活主题应重定向：status=%d", code)
	}

	// 断言：整站页面已转挂 B、快照为 B 的外观、全部标记待重建。
	for _, p := range []*pagedto.PageResp{pageA, pageB} {
		detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: p.ID})
		if err != nil {
			t.Fatalf("查询页面失败: %v", err)
		}
		if detail.ThemeID != themeB.ID {
			t.Errorf("页面应转挂到新激活主题 B：got=%s want=%s", detail.ThemeID, themeB.ID)
		}
		assertThemeSnapshotX(t, detail.DraftDocument, "#2563eb", "Serif")
		assertStructureSnapshotX(t, detail.DraftDocument, "new-hdr", "new-ftr")
		if !detail.Stale {
			t.Errorf("激活后页面应标记待重建：id=%s", p.ID)
		}
		// 展示层快照刷新不应 bump 内容版本（审计 M5 取舍，见报告）。
		if detail.DraftVersion != p.DraftVersion {
			t.Errorf("主题换皮不应改动 draft_version：got=%d want=%d", detail.DraftVersion, p.DraftVersion)
		}
	}
}

// TestActivateThemeThenSaveKeepsNewTheme 激活 B 后再次保存页面草稿：
// mergeActiveTheme 取当前激活主题（B），快照保持一致，不回流到 A。
func TestActivateThemeThenSaveKeepsNewTheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projects, projectID := makeJsonbPageService(t)
	ctx := context.Background()

	if _, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "A",
		Settings: json.RawMessage(`{"colors":{"primary":"#aaaaaa"}}`),
	}); err != nil {
		t.Fatalf("创建主题 A 失败: %v", err)
	}
	themeB, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "B",
		Settings: json.RawMessage(`{"colors":{"primary":"#09de63"}}`),
	})
	if err != nil {
		t.Fatalf("创建主题 B 失败: %v", err)
	}
	page := createJsonbPage(t, svc, projectID, "/reskin-save", reskinDoc)
	db.Table("pages").Where("id = ?", page.ID).Update("stale", false)

	if code := activateThemeHTTP(t, svc, projects, themeB.ID); code != http.StatusSeeOther {
		t.Fatalf("激活主题应重定向：status=%d", code)
	}
	// 再保存一次：保存时快照取当前激活主题（B），应保持 B 的偏色。
	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: page.DraftVersion,
		DraftPath: "/reskin-save", DraftDocument: json.RawMessage(reskinDoc),
	})
	if err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	assertThemeSnapshotX(t, saved.DraftDocument, "#09de63", "")
}

// activateThemeHTTP 构造 dashboard 主题管理 handler，POST 激活指定主题，
// 覆盖 ActivateTheme + reskinProjectPages 编排；返回响应状态码。
func activateThemeHTTP(t *testing.T, svc pagecontract.PageService, projects *projectservice.Service,
	themeID string) int {
	t.Helper()
	// ActivateTheme 的换皮编排已随页面回 project 模块（themeAdminHandle）：
	// 直挂 handler（不经 SetupProjectPages —— 那里带 Casbin 中间件，裸测试引擎没有鉴权链）。
	themes := projecthttp.NewThemeAdminHandle(projects, svc, nil)
	router := gin.New()
	router.POST("/admin/themes/activate", themes.ActivateTheme)

	form := url.Values{"id": {themeID}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/themes/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	return recorder.Code
}

// createJsonbPage 创建指定路径的页面并返回投影。
func createJsonbPage(t *testing.T, svc pagecontract.PageService, projectID, path, doc string) *pagedto.PageResp {
	t.Helper()
	created, err := svc.Create(context.Background(), &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: path, DraftDocument: json.RawMessage(doc),
	})
	if err != nil {
		t.Fatalf("创建页面(%s)失败: %v", path, err)
	}
	return created
}

// reskinDoc 最小合法页面文档（空 root）。
const reskinDoc = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// ---- 断言辅助（本用例局部实现，避免与 unit 包 helper 撞名）----

// assertThemeSnapshotX 断言 settings.theme 快照的偏色与字体。
func assertThemeSnapshotX(t *testing.T, doc []byte, wantPrimary, wantFont string) {
	t.Helper()
	var page struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(doc, &page); err != nil {
		t.Fatalf("解析文档失败: %v", err)
	}
	raw, ok := page.Settings["theme"]
	if !ok {
		t.Fatalf("文档缺少 settings.theme 快照: %s", doc)
	}
	var theme struct {
		Colors     map[string]string `json:"colors"`
		Typography struct {
			Body struct {
				FontFamily string `json:"fontFamily"`
			} `json:"body"`
		} `json:"typography"`
	}
	if err := json.Unmarshal(raw, &theme); err != nil {
		t.Fatalf("解析 theme 快照失败: %v", err)
	}
	if theme.Colors["primary"] != wantPrimary || theme.Typography.Body.FontFamily != wantFont {
		t.Errorf("theme 快照错误: primary=%q font=%q (want %q/%q)",
			theme.Colors["primary"], theme.Typography.Body.FontFamily, wantPrimary, wantFont)
	}
}

// assertStructureSnapshotX 断言 settings.structure 页眉/页脚绑定。
func assertStructureSnapshotX(t *testing.T, doc []byte, wantHeader, wantFooter string) {
	t.Helper()
	var page struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(doc, &page); err != nil {
		t.Fatalf("解析文档失败: %v", err)
	}
	raw, ok := page.Settings["structure"]
	if !ok {
		t.Fatalf("文档缺少 settings.structure 快照: %s", doc)
	}
	var structure struct {
		HeaderBlockID string `json:"headerBlockId"`
		FooterBlockID string `json:"footerBlockId"`
	}
	if err := json.Unmarshal(raw, &structure); err != nil {
		t.Fatalf("解析 structure 快照失败: %v", err)
	}
	if structure.HeaderBlockID != wantHeader || structure.FooterBlockID != wantFooter {
		t.Errorf("structure 快照错误: header=%q footer=%q (want %q/%q)",
			structure.HeaderBlockID, structure.FooterBlockID, wantHeader, wantFooter)
	}
}
