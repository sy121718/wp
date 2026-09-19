package feature

// content_template_admin_pages_test.go — 内容模板列表页的后台改造（EDT-014 生效状态 + EDT-021 切换）。
//
// 覆盖三件在页面上才能看出对错的事：
//   · 结构模板（页眉 / 页脚）与内容实体模板在列表里分得开（类型列 + 徽标）；
//   · 「当前生效」徽标与「设为生效」按钮成对出现（同一（工程, 类型）只有一套生效）；
//   · 引用反查未装配时显示「引用未知」，**不是**「无引用」—— 后者会让人以为可以放心删。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

// contentTemplateAdminDoc 结构模板的合法文档（无字段绑定：结构模板不接受绑定）。
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
	products := productservice.NewService(productmodel.NewModel(db), projects)
	templatesSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := contenttemplatehttp.NewContentTemplatePageHandle(templatesSvc, projects, products, nil)
	engine.GET("/admin/content-templates", handle.ContentTemplatesPage)
	engine.POST("/admin/content-templates/activate", handle.ContentTemplatesActivate)
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

	// 先把「备用页眉」设为生效：同一（工程, 类型）只能有一套生效。
	if _, err := env.templates.Activate(ctx, &contenttemplatedto.ActivateReq{ID: second.ID}); err != nil {
		t.Fatalf("切换生效失败：%v", err)
	}

	body := env.getPage(t)
	for _, want := range []string{
		"当前生效",         // 生效徽标
		"设为生效",         // 非生效那套的切换按钮
		"页眉（结构）",       // 结构模板与内容实体模板在类型列上分得开
		"页脚（结构）",
		"/admin/content-templates/activate", // 切换按钮真的指向页面入口
		"引用未知",         // 端口未装配 → 不是「无引用」
		"结构模板暂无可视化编辑入口", // 结构模板没有样例实体，说清缺什么
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页缺少 %q", want)
		}
	}
	if strings.Contains(body, "无引用") {
		t.Error("引用能力未装配时不得显示「无引用」（那是「查不出来」而不是「没有引用」）")
	}

	// 切换：把第一套设为生效 → 302 回列表（带 done 回执）。
	form := url.Values{"id": {first.ID}, "projectId": {env.projectID}}
	req := httptest.NewRequest(http.MethodPost, "/admin/content-templates/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("切换生效应 302，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	assertNoContentTemplateInternalLeak(t, "302 Location", loc)
	u, perr := url.Parse(loc)
	if perr != nil {
		t.Fatalf("Location 无法解析：%v（%s）", perr, loc)
	}
	if u.Query().Get("done") == "" {
		t.Fatalf("切换成功应回带 ?done=，实际 %s", loc)
	}
	if u.Query().Get("project") != env.projectID {
		t.Fatalf("回跳应保留工程筛选，实际 %s", loc)
	}

	// 落库校验：生效的那套换了，旧的必须已经置 false（同一类型只有一套生效）。
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
	req := httptest.NewRequest(http.MethodPost, "/admin/content-templates/activate",
		strings.NewReader(url.Values{"projectId": {env.projectID}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("缺 id 应 302 回列表，实际 %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	assertNoContentTemplateInternalLeak(t, "302 Location", loc)
	if !strings.Contains(loc, "err=") {
		t.Fatalf("缺 id 应回带 ?err=，实际 %s", loc)
	}
}
