package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/adminui"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

const contentTemplateAdminDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"结构"}}]}`

type contentTemplateAdminEnv struct {
	engine    *gin.Engine
	db        *gorm.DB
	projectID string
	templates *contenttemplateservice.Service
}

func newContentTemplateAdminEnv(t *testing.T) *contentTemplateAdminEnv {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "结构模板后台页测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败：%v", err)
	}
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	templatesSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := contenttemplatehttp.NewContentTemplatePageHandle(templatesSvc, projects, products, nil)
	engine.GET("/admin/content-templates",
		adminui.PageSlot(contenttemplatehttp.ContentTemplatesPageTemplate),
		handle.ContentTemplatesPage)
	engine.POST("/api/contenttemplate/activate", contenttemplatehttp.NewHandle(templatesSvc).Activate)
	return &contentTemplateAdminEnv{engine: engine, db: db, projectID: project.ID, templates: templatesSvc}
}

func (e *contentTemplateAdminEnv) create(t *testing.T, name, entityType string) *contenttemplatedto.TemplateResp {
	t.Helper()
	res, err := e.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: entityType, Name: name,
		DraftDocument: []byte(contentTemplateAdminDoc), ProjectID: e.projectID,
	})
	if err != nil {
		t.Fatalf("创建模板 %s 失败：%v", name, err)
	}
	return res
}

func (e *contentTemplateAdminEnv) getPage(t *testing.T) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/content-templates?project="+e.projectID, nil)
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页应 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoContentTemplateInternalLeak(t, "内容模板列表页 HTML", body)
	if !strings.Contains(body, "</html>") {
		t.Fatalf("页面未渲染完整（缺 </html>）：%s", body)
	}
	return body
}

func TestContentTemplatesPageShowsStructureStateAndActivate(t *testing.T) {
	env := newContentTemplateAdminEnv(t)
	if env == nil {
		return
	}
	ctx := context.Background()
	first := env.create(t, "站点页眉", contenttemplatemodel.EntityTypeHeader)
	second := env.create(t, "备用页眉", contenttemplatemodel.EntityTypeHeader)
	env.create(t, "页脚", contenttemplatemodel.EntityTypeFooter)

	if _, err := env.templates.Activate(ctx, &contenttemplatedto.ActivateReq{ID: second.ID}); err != nil {
		t.Fatalf("切换生效失败：%v", err)
	}

	body := env.getPage(t)
	for _, want := range []string{
		"当前生效",
		"设为生效",
		"页眉（结构模板）",
		"页脚（结构模板）",
		"/api/contenttemplate/activate",
		"引用未知",
		"/workbench?entityType=header",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页缺少 %q", want)
		}
	}
	if strings.Contains(body, "无引用") {
		t.Error("引用能力未装配时不得显示「无引用」（那是「查不出来」而不是「没有引用」）")
	}

	if strings.Contains(body, "无可视化编辑入口") {
		t.Error("结构模板应已打通可视化编辑入口")
	}
	if strings.Contains(body, "entityId=") {
		t.Error("结构模板的编辑入口不该带样例实体参数（无实体模式）")
	}

	payload, err := json.Marshal(map[string]string{"id": first.ID})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/contenttemplate/activate",
		strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("切换生效应渲染提示页（200），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	assertNoContentTemplateInternalLeak(t, "启用 JSON 回执", rec.Body.String())
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatal("expected JSON")
	}

	gotFirst, err := env.templates.Get(ctx, &contenttemplatedto.GetReq{ID: first.ID})
	if err != nil {
		t.Fatalf("读取第一套模板失败：%v", err)
	}
	gotSecond, err := env.templates.Get(ctx, &contenttemplatedto.GetReq{ID: second.ID})
	if err != nil {
		t.Fatalf("读取第二套模板失败：%v", err)
	}
	if !gotFirst.IsDefault || gotSecond.IsDefault {
		t.Fatalf("切换后应第一套生效、第二套失效，实际 first=%v second=%v",
			gotFirst.IsDefault, gotSecond.IsDefault)
	}
}

func TestContentTemplatesActivateRejectsMissingID(t *testing.T) {
	env := newContentTemplateAdminEnv(t)
	if env == nil {
		return
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/contenttemplate/activate",
		strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing id status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertNoContentTemplateInternalLeak(t, "缺 id JSON 回执", rec.Body.String())
}
