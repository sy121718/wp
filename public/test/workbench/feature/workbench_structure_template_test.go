package feature

// workbench_structure_template_test.go — 结构模板（页眉 / 页脚）的**无样例实体**编辑模式。
//
// 钉住两件事：
//  1. 结构模板能进工作台：不带 entityId 也能打开编辑器，预览走「无字段绑定」渲染并产出
//     非空 HTML（且草稿真正参与渲染 —— 画布所见即未保存的编辑内容）；
//  2. 放宽只对结构类型生效：内容实体模板（article）**仍然**要求样例实体，缺它直接 400。
//     否则「结构模板不需要实体」会被读成「模板预览都不需要实体」，字段绑定预览退化成
//     一片空白组件，且没有任何报错。
//
// 渲染路径的选择（解释也写在工作台侧的 renderStructureTemplatePreview）：结构模板走 page
// 编译管线（page.CompilePreview，与页面 / 全局块画布同一入口），而不是 presentation.PreviewInstance
// —— 后者整条链以实体为前提（ValidateFieldRefs / ResolverFor / applyEntitySEO）。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder/core"
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

// workbenchStructureDoc 结构模板文档（无字段绑定）+ 一个可断言的内容标记。
const workbenchStructureDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"no-entity-mark","props":{"text":"STRUCTURE-NO-ENTITY-MARK"}}]}`

// workbenchStructureDraftDoc 草稿覆盖后的标记：用来证明 POST 预览渲染的是**未保存的草稿**
// 而不是已存版本。
const workbenchStructureDraftDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.text","id":"no-entity-mark","props":{"text":"STRUCTURE-DRAFT-MARK"}}]}`

type workbenchTemplateEnv struct {
	engine    *gin.Engine
	projectID string
	templates *contenttemplateservice.Service
	// preview 预览端口桩：装配齐（生产装配必然注入）但**不应被调用** ——
	// 缺样例实体的请求要在参数校验处就 400，而不是落到端口里再失败。
	preview *workbenchRecorderPreviewPort
}

// workbenchRecorderPreviewPort 记录调用的模板预览端口桩。
type workbenchRecorderPreviewPort struct{ calls int }

func (p *workbenchRecorderPreviewPort) PreviewInstance(_ context.Context,
	_ *presentationdto.PreviewInstanceReq) (*presentationdto.PreviewInstanceResp, error) {
	p.calls++
	return nil, errors.New("预览端口不应被调用")
}

// workbenchStubEntitySource 内容实体类型桩：只提供类型标识，不提供字段解析器
// （与 contenttemplate unit 包的 testRegistry 同形 —— 本模块只关心「类型合法性来自注册表」）。
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
	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, nil, nil, nil, nil, nil)

	// 工作台处理器直挂（页面路由本来只挂 Session + CSRF + 权限上下文，业务鉴权不在页面组）：
	// 本用例测的是模板目标的分流与渲染，不是中间件链。
	previewPort := &workbenchRecorderPreviewPort{}
	h := workbenchhttp.New(pages, projects, nil, nil, nil, templatesSvc, previewPort)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/workbench", h.Workbench)
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

// TestStructureTemplateOpensEditorWithoutEntity 结构模板不带 entityId 打开编辑器与预览。
func TestStructureTemplateOpensEditorWithoutEntity(t *testing.T) {
	env := newWorkbenchTemplateEnv(t)
	if env == nil {
		return
	}
	headerID := env.create(t, "站点页眉", contenttemplatemodel.EntityTypeHeader, workbenchStructureDoc)

	// 1) 编辑器外壳：无 entityId 也必须 200，且画布指向模板预览入口。
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

	// 2) GET 预览：按模板文档本身编译，产出非空 HTML。
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

	// 3) POST 草稿预览：渲染的是未保存草稿（画布所见即编辑内容）。
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

// TestContentEntityTemplateStillRequiresSampleEntity 内容实体模板**仍然**要求样例实体。
//
// 这条是上一条的反面：放宽无实体模式时最容易发生的事故是「连普通模板也不再要 entityId」，
// 而症状是字段绑定渲染成空白组件 —— 画布看起来「渲染成功了」，实际什么都没解析。
func TestContentEntityTemplateStillRequiresSampleEntity(t *testing.T) {
	env := newWorkbenchTemplateEnv(t)
	if env == nil {
		return
	}
	articleID := env.create(t, "文章详情模板", "article", workbenchStructureDoc)

	// 编辑器外壳：缺 entityId → 400。
	rec := env.get(t, "/workbench?template="+articleID+"&entityType=article&projectId="+env.projectID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("内容实体模板缺样例实体应 400，实际 %d，body=%s", rec.Code, firstN(rec.Body.String(), 400))
	}
	if !strings.Contains(rec.Body.String(), "entityId") {
		t.Errorf("400 应说明缺的是 entityId：%s", rec.Body.String())
	}

	// GET 预览：同样缺 entityId → 400（不因放宽而放行）。
	if rec := env.get(t, "/workbench/template/preview?template="+articleID+
		"&entityType=article&projectId="+env.projectID); rec.Code != http.StatusBadRequest {
		t.Fatalf("内容实体模板预览缺 entityId 应 400，实际 %d", rec.Code)
	}

	// POST 草稿预览：同样 400。
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

	// 反向确认：结构类型的判定不来自查询参数 —— 拿 article 模板冒充 header 也不放行。
	rec = env.get(t, "/workbench?template="+articleID+"&entityType=header&projectId="+env.projectID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("entityType 查询参数不得把普通模板变成无实体模式，实际 %d", rec.Code)
	}
	// 三次缺样例实体的请求都不该走到预览端口：否则「400」可能只是端口的副作用。
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
