package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
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
	"go_wp/internal/templates"

	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func makeJsonbPageService(t *testing.T) (*gorm.DB, pagecontract.PageService, *projectservice.Service, string) {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

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

func TestActivateThemeReskinsWholeSite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, svc, projects, projectID := makeJsonbPageService(t)
	ctx := context.Background()

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

	pageA := createJsonbPage(t, svc, projectID, "/reskin-a", reskinDoc)
	pageB := createJsonbPage(t, svc, projectID, "/reskin-b", reskinDoc)
	for _, p := range []*pagedto.PageResp{pageA, pageB} {
		if p.ThemeID != themeA.ID {
			t.Fatalf("页面应挂接主题 A: %+v", p)
		}
		assertThemeSnapshotX(t, p.DraftDocument, "#111111", "Arial")
		assertStructureSnapshotX(t, p.DraftDocument, "", "")

		db.Table("pages").Where("id = ?", p.ID).Update("stale", false)
	}

	if code := activateThemeHTTP(t, svc, projects, themeB.ID); code != http.StatusOK {
		t.Fatalf("激活主题应渲染提示页：status=%d", code)
	}

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

		if detail.DraftVersion != p.DraftVersion {
			t.Errorf("主题换皮不应改动 draft_version：got=%d want=%d", detail.DraftVersion, p.DraftVersion)
		}
	}
}

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

	if code := activateThemeHTTP(t, svc, projects, themeB.ID); code != http.StatusOK {
		t.Fatalf("激活主题应渲染提示页：status=%d", code)
	}

	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: page.DraftVersion,
		DraftPath: "/reskin-save", DraftDocument: json.RawMessage(reskinDoc),
	})
	if err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	assertThemeSnapshotX(t, saved.DraftDocument, "#09de63", "")
}

func activateThemeHTTP(t *testing.T, svc pagecontract.PageService, projects *projectservice.Service,
	themeID string) int {
	t.Helper()

	themes := projecthttp.NewThemeAdminHandle(projects, svc, nil)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.POST("/admin/themes/activate", themes.ActivateTheme)

	form := url.Values{"id": {themeID}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/themes/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	return recorder.Code
}

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

const reskinDoc = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

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
