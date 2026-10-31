package feature

import (
	"context"
	"go_wp/internal/adminui"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	workbenchhttp "go_wp/internal/module/workbench/inbound/http"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

type stubPresentationInstances struct {
	presentationcontract.PresentationService
	inst *presentationdto.InstanceResp
}

func (s stubPresentationInstances) Get(_ context.Context, _ *presentationdto.GetReq) (*presentationdto.InstanceResp, error) {
	return s.inst, nil
}

type layoutRenderEnv struct {
	engine     *gin.Engine
	projectID  string
	pageID     string
	blockID    string
	templateID string
	instanceID string
}

const layoutPageDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"t1","props":{"text":"布局回归标记"}}]}`

func newLayoutRenderEnv(t *testing.T) *layoutRenderEnv {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "工作台布局回归工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败：%v", err)
	}
	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)
	page, err := pages.Create(ctx, &pagedto.CreateReq{
		ProjectID: project.ID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/layout-render", DraftDocument: json.RawMessage(layoutPageDoc),
	})
	if err != nil {
		t.Fatalf("创建测试页面失败：%v", err)
	}
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	block, err := blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: project.ID, Name: "布局回归块", Document: json.RawMessage(layoutPageDoc),
	})
	if err != nil {
		t.Fatalf("创建测试块失败：%v", err)
	}
	templatesSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, core.NewEntitySourceRegistry())
	tpl, err := templatesSvc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: contenttemplatemodel.EntityTypeHeader, Name: "布局回归页眉",
		ProjectID: project.ID, DraftDocument: []byte(layoutPageDoc),
	})
	if err != nil {
		t.Fatalf("创建测试模板失败：%v", err)
	}
	h := workbenchhttp.New(pages, projects, blocks, nil, nil, templatesSvc, nil)
	instanceID := "11111111-2222-3333-4444-555555555555"
	h.SetInstanceOverrideDeps(stubPresentationInstances{inst: &presentationdto.InstanceResp{
		ID: instanceID, ProjectID: project.ID, EntityType: "product", EntityID: "p-1",
		RenderMode: presentationdto.RenderModeDocument, Document: json.RawMessage(layoutPageDoc),
	}})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/workbench", adminui.PageSlot(workbenchhttp.CanvasLayoutTemplate), h.Workbench)
	return &layoutRenderEnv{
		engine: engine, projectID: project.ID, pageID: page.ID,
		blockID: block.ID, templateID: tpl.ID, instanceID: instanceID,
	}
}

func assertLayoutRenderedFully(t *testing.T, mode string, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s：状态码应为 200，实际 %d，body=%s", mode, rec.Code, firstN(rec.Body.String(), 300))
	}
	body := rec.Body.String()

	for _, needle := range []string{"</html>", "id=\"wb-canvas\"", "id=\"wb-save-draft\"", "id=\"wb-bootstrap\"", "id=\"wb-meta\"", "wb-canvas-toolbar", "id=\"wb-sidebar-restore\"", "data-tab=\"layout\"", "data-tab=\"motion\""} {
		if !strings.Contains(body, needle) {
			t.Fatalf("%s：页面未渲染完整（缺 %q，共 %d 字节）—— 典型的「200 + 静默截断」，多半是 layout.html 里某个可选键没走 isset：%s", mode, needle, len(body), firstN(body, 400))
		}
	}

	t.Logf("%s：整页渲染完整，共 %d 字节", mode, len(body))
	return body
}

func TestWorkbenchLayoutRendersFullyInAllModes(t *testing.T) {
	env := newLayoutRenderEnv(t)
	if env == nil {
		return
	}
	assert := func(t *testing.T, mode, target string, wants ...string) {
		t.Helper()
		rec := httptest.NewRecorder()
		env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		body := assertLayoutRenderedFully(t, mode, rec)
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Fatalf("%s：缺少该模式应有的标记 %q", mode, want)
			}
		}
	}

	t.Run("页面模式", func(t *testing.T) {
		assert(t, "页面模式", "/workbench?id="+env.pageID, "id=\"wb-publish\"", "/workbench/preview?id=")
	})
	t.Run("块模式", func(t *testing.T) {
		assert(t, "块模式", "/workbench?block="+env.blockID, "/workbench/block/preview?id=")
	})
	t.Run("模板模式", func(t *testing.T) {
		assert(t, "模板模式", "/workbench?template="+env.templateID+"&projectId="+env.projectID,
			"id=\"wb-template-mode\"", "/workbench/template/preview?")
	})
	t.Run("实例模式", func(t *testing.T) {
		assert(t, "实例模式", "/workbench?instance="+env.instanceID, "id=\"wb-instance-mode\"", "instance=")
	})
}
