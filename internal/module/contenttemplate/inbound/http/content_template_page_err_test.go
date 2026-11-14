package contenttemplatehttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

const ctProjectListErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

type ctFailingProjects struct {
	projectcontract.ProjectService
}

func (ctFailingProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, errors.New(ctProjectListErrText)
}

type ctCalls struct {
	impact  int
	list    int
	product int
	content int
}

type ctCountingTemplates struct {
	contenttemplatecontract.ContentTemplateService
	calls *ctCalls
}

func (s ctCountingTemplates) Impact(context.Context, *contenttemplatedto.ImpactReq) (*contenttemplatedto.ImpactResp, error) {
	s.calls.impact++
	return &contenttemplatedto.ImpactResp{Available: true}, nil
}

func (s ctCountingTemplates) List(context.Context, *contenttemplatedto.ListReq) ([]*contenttemplatedto.TemplateResp, error) {
	s.calls.list++
	return []*contenttemplatedto.TemplateResp{}, nil
}

type ctCountingProducts struct {
	productcontract.ProductService
	calls *ctCalls
}

func (s ctCountingProducts) List(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error) {
	s.calls.product++
	return nil, nil
}

type ctCountingContents struct {
	contentcontract.ContentService
	calls *ctCalls
}

func (s ctCountingContents) List(context.Context, *contentdto.ListReq) ([]*contentdto.ContentResp, error) {
	s.calls.content++
	return nil, nil
}

func TestContentTemplatesPageProjectListFailureReturnsJSON(t *testing.T) {
	rawErr := errors.New(ctProjectListErrText)
	calls := &ctCalls{}

	rec := renderContentTemplatesPageWithDeps(t, ctCountingTemplates{calls: calls}, ctFailingProjects{},
		ctCountingProducts{calls: calls}, ctCountingContents{calls: calls},
		"/admin/content-templates?entityType=header")
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("工程列表读不到（主数据失败）应为 500，实际 %d：%s", rec.Code, body)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("失败出口应为 JSON（Content-Type 含 application/json），得到 %q", ct)
	}
	if strings.Contains(body, "<!DOCTYPE") || strings.Contains(body, "</html>") {
		t.Errorf("失败出口不该渲染页壳：%s", body)
	}

	if !strings.Contains(body, response.InternalFallback) {
		t.Errorf("失败响应应带归口文案，实际：%s", body)
	}
	for _, forbidden := range []string{
		"uiInternalErr",
		rawErr.Error(),
		ctProjectListErrText,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("失败响应不应出现 %q：%s", forbidden, body)
		}
	}

	if calls.impact != 0 || calls.list != 0 || calls.product != 0 || calls.content != 0 {
		t.Errorf("工程失败后不该再查引用面/清单/样例实体，实际 impact=%d list=%d product=%d content=%d",
			calls.impact, calls.list, calls.product, calls.content)
	}

	if !ctTestLogHasRawError(t, rawErr) {
		t.Errorf("日志里没有 error=%q 的结构化记录 —— 归口变成了丢弃", rawErr.Error())
	}
}

const ctTemplateListErrText = `pq: relation "content_templates" does not exist (SQLSTATE 42P01)`

type ctOKProjects struct {
	projectcontract.ProjectService
}

func (ctOKProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return []projectcontract.ProjectResp{{ID: "project-1", Name: "默认站点"}}, nil
}

type ctFailingTemplates struct {
	contenttemplatecontract.ContentTemplateService

	err error
}

func (ctFailingTemplates) Impact(context.Context, *contenttemplatedto.ImpactReq) (*contenttemplatedto.ImpactResp, error) {
	return &contenttemplatedto.ImpactResp{Available: true}, nil
}

func (s ctFailingTemplates) List(context.Context, *contenttemplatedto.ListReq) ([]*contenttemplatedto.TemplateResp, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, errors.New(ctTemplateListErrText)
}

func TestContentTemplatesPageMainListFailureReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handle := NewContentTemplatePageHandle(ctFailingTemplates{}, ctOKProjects{}, nil, nil)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/content-templates",
		pageview.Slot(ContentTemplatesPageTemplate, pageview.RenderPage),
		handle.ContentTemplatesPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/content-templates", nil))
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据读不到应为 500，实际 %d：%s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("失败出口应为 JSON，得到 %q", ct)
	}
	if strings.Contains(body, "</html>") || strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("失败出口不该渲染页壳：%s", body)
	}

	if !strings.Contains(body, response.InternalFallback) {
		t.Errorf("失败响应应带归口文案：%s", body)
	}
	for _, forbidden := range []string{
		"uiInternalErr", "还没有内容模板",
		"SQLSTATE", "42P01", "content_templates\"",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("失败响应不应出现 %q", forbidden)
		}
	}
}

type ctTemplatesStub struct {
	contenttemplatecontract.ContentTemplateService
	list []*contenttemplatedto.TemplateResp
}

func (ctTemplatesStub) Impact(context.Context, *contenttemplatedto.ImpactReq) (*contenttemplatedto.ImpactResp, error) {
	return &contenttemplatedto.ImpactResp{Available: true}, nil
}

func (s ctTemplatesStub) List(context.Context, *contenttemplatedto.ListReq) ([]*contenttemplatedto.TemplateResp, error) {
	return s.list, nil
}

var ctPageTestLogDirs sync.Map

func renderContentTemplatesPage(t *testing.T, svc contenttemplatecontract.ContentTemplateService) *httptest.ResponseRecorder {
	t.Helper()
	return renderContentTemplatesPageLang(t, svc, "")
}

func renderContentTemplatesPageWithDeps(t *testing.T, templatesSvc contenttemplatecontract.ContentTemplateService,
	projects projectcontract.ProjectService, products productcontract.ProductService,
	contents contentcontract.ContentService, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	logDir := filepath.Join(t.TempDir(), "logs")
	ctPageTestLogDirs.Store(t.Name(), logDir)
	conf := viper.New()
	conf.Set("log.base_dir", logDir)
	if err := logger.Init(conf); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = logger.Close() })

	handle := NewContentTemplatePageHandle(templatesSvc, projects, products, contents)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/content-templates",
		pageview.Slot(ContentTemplatesPageTemplate, pageview.RenderPage),
		handle.ContentTemplatesPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func renderContentTemplatesPageLang(t *testing.T, svc contenttemplatecontract.ContentTemplateService, lang string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/admin/content-templates"
	if lang != "" {
		target += "?lang=" + lang
	}
	return renderContentTemplatesPageWithDeps(t, svc, ctOKProjects{}, nil, nil, target)
}

func TestContentTemplatesPageSuccessRendersList(t *testing.T) {
	rec := renderContentTemplatesPage(t, ctTemplatesStub{list: []*contenttemplatedto.TemplateResp{
		{ID: "tpl-1", Name: "站点页眉模板", EntityType: "header", DraftVersion: 3},
	}})
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("成功应为 200，实际 %d：%s", rec.Code, body)
	}
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "no-store") {
		t.Errorf("成功页不该带 no-store，得到 %q", cc)
	}
	for _, want := range []string{"</html>", "站点页眉模板"} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页应含 %q", want)
		}
	}
	for _, forbidden := range []string{"列表没读出来", "还没有内容模板"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("有数据时不该出现失败/空态文案 %q", forbidden)
		}
	}
}

func TestContentTemplatesPageEmptyRendersEmptyState(t *testing.T) {
	rec := renderContentTemplatesPage(t, ctTemplatesStub{})
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("空列表应为 200，实际 %d：%s", rec.Code, body)
	}
	if !strings.Contains(body, "还没有内容模板") {
		t.Errorf("空列表应显示「还没有内容模板」：%s", body)
	}
	if strings.Contains(body, "列表没读出来") {
		t.Errorf("空列表不是装载失败，不该显示失败文案")
	}

	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "no-store") {
		t.Errorf("成功空态不该带 no-store（那是失败页的缓存语义），得到 %q", cc)
	}
}

const ctInternalErrorKey = response.InternalFallback

func TestContentTemplatesPageMainListFailureRendersEnglishWhenTermPresent(t *testing.T) {

	i18n.InjectForTest(map[string]map[string]string{
		response.InternalKey: {"en-US": "EN-INTERNAL-ERROR"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rec := renderContentTemplatesPageLang(t, ctFailingTemplates{}, "en-US")
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主列表读不到应为 500，实际 %d：%s", rec.Code, body)
	}
	if !strings.Contains(body, "EN-INTERNAL-ERROR") {
		t.Errorf("英文请求应出现词条译文：%s", body)
	}
	for _, forbidden := range []string{
		contentTemplateErrInternalFallback, response.InternalKey,
		ctTemplateListErrText, html.EscapeString(ctTemplateListErrText),
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("英文请求不应出现 %q", forbidden)
		}
	}
}

func TestContentTemplatesPageMainListFailureFallsBackWhenTermMissing(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rec := renderContentTemplatesPageLang(t, ctFailingTemplates{}, "en-US")
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主列表读不到应为 500，实际 %d：%s", rec.Code, body)
	}

	if !strings.Contains(body, ctInternalErrorKey) {
		t.Errorf("缺词条时应落中文兜底 %q：%s", ctInternalErrorKey, body)
	}
	for _, forbidden := range []string{ctTemplateListErrText, html.EscapeString(ctTemplateListErrText)} {
		if strings.Contains(body, forbidden) {
			t.Errorf("缺词条时不应出现 %q", forbidden)
		}
	}
}

func TestContentTemplatesPageLoadFailureKeepsRawErrorInLog(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rawErr := errors.New(`pq: relation "content_templates" does not exist (SQLSTATE 42P01)`)
	rec := renderContentTemplatesPage(t, ctFailingTemplates{err: rawErr})

	body := rec.Body.String()
	for _, leaked := range []string{rawErr.Error(), html.EscapeString(rawErr.Error())} {
		if strings.Contains(body, leaked) {
			t.Fatalf("归口失败：原文进了页面：%s", body)
		}
	}

	if !ctTestLogHasRawError(t, rawErr) {
		t.Errorf("日志里没有 error=%q 的结构化记录 —— 归口变成了丢弃", rawErr.Error())
	}

	if ctTestLogHasRawError(t, errors.New(`pq: relation "never_happened_ct" does not exist`)) {
		t.Errorf("自检失败：本次没发生过的原文也被判定为命中")
	}
}

func ctTestLogDir(t *testing.T) string {
	t.Helper()
	value, ok := ctPageTestLogDirs.Load(t.Name())
	if !ok {
		t.Fatalf("用例 %s 尚未装配测试路由，日志目录未知", t.Name())
	}
	return value.(string)
}

func ctTestLogHasRawError(t *testing.T, rawErr error) bool {
	t.Helper()
	want := rawErr.Error()
	root := ctTestLogDir(t)
	found := false
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if !strings.Contains(filepath.ToSlash(path), "/"+contentTemplateErrScene+"/") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var record map[string]any
			if json.Unmarshal([]byte(line), &record) != nil {
				continue
			}
			if got, ok := record["error"].(string); ok && got == want {
				found = true
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("遍历日志目录 %s 失败：%v", root, err)
	}
	return found
}

// 「设为生效」必须是独立端点按钮、不位于任何表单内：htmx 提交时用
// new FormData(最近的表单)，混在批量删除表单里会把勾选行的 ids 带进激活请求，
// 而 ActivateReq 只认 id。这里在渲染结果上钉住结构，防止重构悄悄退化。
func TestContentTemplatesActivateButtonIsNotInsideDeleteForm(t *testing.T) {
	rec := renderContentTemplatesPage(t, ctTemplatesStub{list: []*contenttemplatedto.TemplateResp{
		{ID: "tpl-1", Name: "站点页眉模板", EntityType: "header", DraftVersion: 3},
		{ID: "tpl-2", Name: "当前生效模板", EntityType: "header", DraftVersion: 1, IsDefault: true},
	}})
	body := rec.Body.String()

	if !strings.Contains(body, `hx-post="/api/contenttemplate/activate"`) {
		t.Fatalf("应渲染激活按钮：%s", body)
	}
	if !strings.Contains(body, `hx-vals='{"id":"tpl-1"}'`) {
		t.Errorf("激活按钮的 id 应由 hx-vals 显式声明")
	}
	// 批量删除表单必须收窄到只包批量条；勾选框靠 form 属性关联。
	deleteForm := strings.Index(body, `<form id="ct-bulk-form"`)
	if deleteForm < 0 {
		t.Fatalf("批量删除表单缺少稳定 id")
	}
	deleteFormEnd := strings.Index(body[deleteForm:], "</form>")
	if deleteFormEnd < 0 {
		t.Fatalf("批量删除表单未闭合")
	}
	inside := body[deleteForm : deleteForm+deleteFormEnd]
	if strings.Contains(inside, "/api/contenttemplate/activate") {
		t.Error("激活入口不得位于批量删除表单内（会带上勾选行的 ids）")
	}
	if !strings.Contains(body, `form="ct-bulk-form"`) {
		t.Error("行勾选框应用 form 属性关联批量删除表单")
	}
}
