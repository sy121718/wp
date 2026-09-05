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
// 自建的 themes/pages 表将 draft_document/settings 声明为 JSONB——
// 唯有 JSONB 才支持 jsonb_set 快照刷新（feature 包共享的 newPageService 用 JSON，
// 与生产 DDL 漂移，故本用例不复用该 helper，避免 jsonb_set 类型错误）。

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
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// makeJsonbPageService 自建 JSONB 测试 schema 并装配 page/project/block 服务，
// 返回 db、pages 契约、projects 服务与工程 ID。
func makeJsonbPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil, nil, nil, ""
	}
	// draft_document / settings / source_document 一律 JSONB（对齐生产迁移
	// init_builder_schema.sql 与 020_themes.sql），保证 jsonb_set 可用。
	for _, statement := range []string{
		`CREATE TABLE projects (id UUID PRIMARY KEY, name TEXT NOT NULL, settings JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE themes (id UUID PRIMARY KEY, project_id UUID NOT NULL, name TEXT NOT NULL, settings JSONB NOT NULL, is_active BOOLEAN NOT NULL, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE pages (id UUID PRIMARY KEY, project_id UUID NOT NULL, theme_id UUID, kind TEXT NOT NULL, content_target_type TEXT NOT NULL, content_target_id UUID, draft_path TEXT NOT NULL, active_path TEXT, draft_document JSONB NOT NULL, draft_version INTEGER NOT NULL, staged_artifact_id UUID, active_artifact_id UUID, stale BOOLEAN NOT NULL, deleted_at TIMESTAMPTZ, published_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`,
		`CREATE TABLE page_revisions (id UUID PRIMARY KEY, page_id UUID NOT NULL, version INTEGER NOT NULL, draft_path TEXT NOT NULL, draft_document JSONB NOT NULL, source_hash TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL, UNIQUE(page_id, version))`,
		`CREATE TABLE page_routes (project_id UUID NOT NULL, path TEXT NOT NULL, page_id UUID, presentation_id UUID, route_kind TEXT NOT NULL, artifact_id UUID, updated_at TIMESTAMPTZ NOT NULL, PRIMARY KEY(project_id, path))`,
		`CREATE TABLE page_artifacts (id UUID PRIMARY KEY, page_id UUID NOT NULL, version INTEGER NOT NULL, source_document JSONB NOT NULL, page_document_schema_version INTEGER NOT NULL, source_hash TEXT NOT NULL, build_input_manifest JSONB NOT NULL, build_input_hash TEXT NOT NULL, artifact_provider TEXT NOT NULL, artifact_key TEXT NOT NULL, artifact_hash TEXT NOT NULL, compiler_version TEXT NOT NULL, registry_version TEXT NOT NULL, manifest JSONB NOT NULL, payload_state TEXT NOT NULL, payload_deleted_at TIMESTAMPTZ, note TEXT NOT NULL, created_by UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL, UNIQUE(page_id, version), UNIQUE(id, page_id))`,
		`CREATE TABLE content_objects (content_hash TEXT PRIMARY KEY, provider TEXT NOT NULL, object_key TEXT NOT NULL, byte_size INTEGER NOT NULL, created_at TIMESTAMPTZ NOT NULL, deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE page_artifact_objects (artifact_id UUID NOT NULL, content_hash TEXT NOT NULL, PRIMARY KEY(artifact_id, content_hash))`,
		`CREATE TABLE publication_receipts (id UUID PRIMARY KEY, source_type TEXT NOT NULL, source_id UUID NOT NULL, action TEXT NOT NULL, path TEXT NOT NULL, from_artifact_id UUID, to_artifact_id UUID, receipt_state TEXT NOT NULL, receipt_data JSONB NOT NULL, created_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	pageModel := pagemodel.NewPageModel(db)
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	svc := pageservice.NewService(pageModel, artifacts, routes, projects, blocks)
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
		Settings: json.RawMessage(`{"colors":{"primary":"#111111"},"fontFamily":"Arial"}`),
	})
	if err != nil {
		t.Fatalf("创建主题 A 失败: %v", err)
	}
	if !themeA.IsActive {
		t.Fatalf("首个主题应自动激活: %+v", themeA)
	}
	themeB, err := projects.CreateTheme(ctx, &projectdto.ThemeCreateReq{
		ProjectID: projectID, Name: "春季暗色",
		Settings: json.RawMessage(`{"colors":{"primary":"#2563eb"},"fontFamily":"Serif","headerBlockId":"new-hdr","footerBlockId":"new-ftr"}`),
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
		detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: p.ID})
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
	handle := dashboardhttp.NewHandle(svc, projects, nil)
	router := gin.New()
	router.POST("/admin/themes/activate", handle.ActivateTheme)

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
		FontFamily string            `json:"fontFamily"`
	}
	if err := json.Unmarshal(raw, &theme); err != nil {
		t.Fatalf("解析 theme 快照失败: %v", err)
	}
	if theme.Colors["primary"] != wantPrimary || theme.FontFamily != wantFont {
		t.Errorf("theme 快照错误: primary=%q font=%q (want %q/%q)",
			theme.Colors["primary"], theme.FontFamily, wantPrimary, wantFont)
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