package feature

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/builder/core"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	workbenchhttp "go_wp/internal/module/workbench/inbound/http"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

const workbenchStructureDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"no-entity-mark","props":{"text":"STRUCTURE-NO-ENTITY-MARK"}}]}`

const workbenchStructureDraftDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"no-entity-mark","props":{"text":"STRUCTURE-DRAFT-MARK"}}]}`

type workbenchTemplateEnv struct {
	engine    *gin.Engine
	projectID string
	templates *contenttemplateservice.Service

	preview *workbenchRecorderPreviewPort
}

type workbenchRecorderPreviewPort struct{ calls int }

func (p *workbenchRecorderPreviewPort) PreviewInstance(_ context.Context,
	_ *presentationdto.PreviewInstanceReq) (*presentationdto.PreviewInstanceResp, error) {
	p.calls++
	return nil, errors.New("预览端口不应被调用")
}

type workbenchStubEntitySource struct{ entityType string }

func (s workbenchStubEntitySource) EntityType() string       { return s.entityType }
func (s workbenchStubEntitySource) FieldWhitelist() []string { return nil }

func (s workbenchStubEntitySource) ResolverFor(_ context.Context, _ string) (core.ContentResolver, error) {
	return nil, errors.New("测试桩不提供字段解析器")
}

func newWorkbenchTemplateEnv(t *testing.T) *workbenchTemplateEnv {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "结构模板工作台测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败：%v", err)
	}
	registry := core.NewEntitySourceRegistry()
	if err := registry.Register(workbenchStubEntitySource{entityType: "article"}); err != nil {
		t.Fatalf("注册实体类型 article 失败：%v", err)
	}
	templatesSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, registry)
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, blocks, nil, nil, nil, nil)

	previewPort := &workbenchRecorderPreviewPort{}
	h := workbenchhttp.New(pages, projects, blocks, nil, nil, templatesSvc, previewPort)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/workbench", adminui.PageSlot(workbenchhttp.CanvasLayoutTemplate), h.Workbench)
	engine.GET("/workbench/template/preview", h.TemplatePreview)
	engine.POST("/workbench/template/preview", h.TemplatePreviewDraft)
	return &workbenchTemplateEnv{engine: engine, projectID: project.ID, templates: templatesSvc, preview: previewPort}
}

func (e *workbenchTemplateEnv) create(t *testing.T, name, entityType, doc string) string {
	t.Helper()
	res, err := e.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: entityType, Name: name, DraftDocument: []byte(doc), ProjectID: e.projectID,
	})
	if err != nil {
		t.Fatalf("创建模板 %s 失败：%v", name, err)
	}
	return res.ID
}

func (e *workbenchTemplateEnv) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestStructureTemplateOpensEditorWithoutEntity(t *testing.T) {
	env := newWorkbenchTemplateEnv(t)
	if env == nil {
		return
	}
	headerID := env.create(t, "站点页眉", contenttemplatemodel.EntityTypeHeader, workbenchStructureDoc)

	rec := env.get(t, "/workbench?template="+headerID+"&entityType=header&projectId="+env.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("结构模板应能无实体打开编辑器，实际 %d，body=%s", rec.Code, firstN(rec.Body.String(), 400))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("编辑器页面未渲染完整（缺 </html>）：%s", firstN(body, 400))
	}
	if !strings.Contains(body, "/workbench/template/preview?") {
		t.Fatalf("编辑器画布未指向模板预览入口：%s", firstN(body, 600))
	}
	if strings.Contains(body, "entityId=") {
		t.Fatalf("无实体模式不该把 entityId 带进画布查询串：%s", firstN(body, 600))
	}
	if !strings.Contains(body, "结构模板") {
		t.Errorf("编辑器应提示当前处于结构模板（不解析字段绑定）模式")
	}

	rec = env.get(t, "/workbench/template/preview?template="+headerID+
		"&entityType=header&projectId="+env.projectID+"&editor=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("结构模板应能无实体预览，实际 %d，body=%s", rec.Code, firstN(rec.Body.String(), 400))
	}
	html := rec.Body.String()
	if !strings.Contains(html, "STRUCTURE-NO-ENTITY-MARK") {
		t.Fatalf("预览产物应包含模板文档内容：%s", firstN(html, 600))
	}
	if !strings.Contains(html, "</body>") {
		t.Fatalf("预览产物应是完整文档：%s", firstN(html, 600))
	}

	form := url.Values{
		"id":            {headerID},
		"entityType":    {"header"},
		"projectId":     {env.projectID},
		"draftDocument": {workbenchStructureDraftDoc},
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/template/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("结构模板草稿预览应 200，实际 %d，body=%s", rec.Code, firstN(rec.Body.String(), 400))
	}
	if got := rec.Body.String(); !strings.Contains(got, "STRUCTURE-DRAFT-MARK") {
		t.Fatalf("草稿预览应渲染传入的草稿文档：%s", firstN(got, 600))
	}
}

func TestContentEntityTemplateStillRequiresSampleEntity(t *testing.T) {
	env := newWorkbenchTemplateEnv(t)
	if env == nil {
		return
	}
	articleID := env.create(t, "文章详情模板", "article", workbenchStructureDoc)

	rec := env.get(t, "/workbench?template="+articleID+"&entityType=article&projectId="+env.projectID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("内容实体模板缺样例实体应 400，实际 %d，body=%s", rec.Code, firstN(rec.Body.String(), 400))
	}
	if !strings.Contains(rec.Body.String(), "entityId") {
		t.Errorf("400 应说明缺的是 entityId：%s", rec.Body.String())
	}

	if rec := env.get(t, "/workbench/template/preview?template="+articleID+
		"&entityType=article&projectId="+env.projectID); rec.Code != http.StatusBadRequest {
		t.Fatalf("内容实体模板预览缺 entityId 应 400，实际 %d", rec.Code)
	}

	form := url.Values{
		"id":            {articleID},
		"entityType":    {"article"},
		"projectId":     {env.projectID},
		"draftDocument": {workbenchStructureDraftDoc},
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/template/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("内容实体模板草稿预览缺 entityId 应 400，实际 %d，body=%s", rec.Code, firstN(rec.Body.String(), 400))
	}

	rec = env.get(t, "/workbench?template="+articleID+"&entityType=header&projectId="+env.projectID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("entityType 查询参数不得把普通模板变成无实体模式，实际 %d", rec.Code)
	}

	if env.preview.calls != 0 {
		t.Fatalf("缺样例实体的请求不应调用预览端口，实际调用 %d 次", env.preview.calls)
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
