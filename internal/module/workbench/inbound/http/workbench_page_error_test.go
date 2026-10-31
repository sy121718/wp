package workbenchhttp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	"go_wp/internal/builder/source"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

type workbenchPageStub struct {
	pagecontract.PageService

	projectErr error

	page *pagedto.PageResp
}

func (s *workbenchPageStub) ProjectOfPage(_ context.Context, _ string) (string, error) {
	if s.projectErr != nil {
		return "", s.projectErr
	}
	return "project-1", nil
}

func (s *workbenchPageStub) Detail(_ context.Context, _ *pagedto.DetailReq) (*pagedto.PageResp, error) {
	if s.page == nil {
		return nil, errors.New("stub: 页面记录不存在")
	}
	return s.page, nil
}

func newWorkbenchPageErrorRouter(t *testing.T, h *Handle) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.GET("/workbench", h.Workbench)
	return router
}

func assertWorkbenchErrorShell(t *testing.T, rec *httptest.ResponseRecorder, body, wantText string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("整页失败出口必须是 HTML（Content-Type 含 text/html），得到 %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("失败页不该被缓存（Cache-Control 含 no-store），得到 %q", cc)
	}

	for _, want := range []string{
		"<!DOCTYPE html>",
		`class="wb-body"`,
		`class="wb-topbar"`,
		`href="/admin"`,
		`id="wb-error-message"`,
		wantText,
		"</body>",
		"</html>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("工作台错误页缺少 %q，实际：%s", want, body)
		}
	}

	slot := errorMessageSlot(body)
	if slot == "" {
		t.Errorf("没有找到消息槽（模板结构变了？）：%s", body)
	} else if !strings.Contains(slot, wantText) {
		t.Errorf("消息槽里的文案应为 %q，实际 %q", wantText, slot)
	}
}

func errorMessageSlot(body string) string {
	i := strings.Index(body, `id="wb-error-message"`)
	if i < 0 {
		return ""
	}
	open := strings.Index(body[i:], ">")
	if open < 0 {
		return ""
	}
	rest := body[i+open+1:]
	if j := strings.Index(rest, "</p>"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestWorkbenchPageErrorRendersFullShellNotBareText(t *testing.T) {
	router := newWorkbenchPageErrorRouter(t, New(&workbenchPageStub{}, nil, nil, nil, nil, nil, nil))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/workbench?lang=zh-CN", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 ?id= 的状态码应为 400（请求侧的事实），得到 %d：%s", rec.Code, body)
	}
	assertWorkbenchErrorShell(t, rec, body, "缺少页面 id")
	if strings.Contains(body, `"code":`) {
		t.Errorf("整页 GET 的失败出口不应是 JSON 报文：%s", body)
	}
}

func TestWorkbenchPageErrorDistinguishesMissingParamPageGoneAndServerFault(t *testing.T) {

	rawErr := errors.New(`pq: relation "pages" does not exist`)

	badDraft := &pagedto.PageResp{
		ID: "page-1", ProjectID: "project-1",
		DraftDocument: json.RawMessage("{ 这不是 JSON"),
	}

	cases := []struct {
		name       string
		stub       *workbenchPageStub
		url        string
		wantStatus int
		wantText   string
	}{
		{"缺参：请求事实", &workbenchPageStub{}, "/workbench?lang=zh-CN", http.StatusBadRequest, "缺少页面 id"},
		{"页面取不到：目标事实", &workbenchPageStub{projectErr: rawErr}, "/workbench?id=page-1&lang=zh-CN", http.StatusNotFound, "页面不存在"},
		{"草稿序列化失败：未知内部异常", &workbenchPageStub{page: badDraft}, "/workbench?id=page-1&lang=zh-CN", http.StatusInternalServerError, "草稿文档序列化失败"},
	}
	allTexts := make([]string, 0, len(cases))
	for _, tc := range cases {
		allTexts = append(allTexts, tc.wantText)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newWorkbenchPageErrorRouter(t, New(tc.stub, nil, nil, nil, nil, nil, nil))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))

			body := rec.Body.String()
			if rec.Code != tc.wantStatus {
				t.Fatalf("状态码应为 %d，得到 %d：%s", tc.wantStatus, rec.Code, body)
			}
			assertWorkbenchErrorShell(t, rec, body, tc.wantText)
			for _, other := range allTexts {
				if other != tc.wantText && strings.Contains(body, other) {
					t.Errorf("三档文案必须互不混同：本页（%s）出现了别档文案 %q", tc.wantText, other)
				}
			}

			for _, leak := range []string{rawErr.Error(), "这不是 JSON", "invalid character"} {
				if strings.Contains(body, leak) {
					t.Errorf("内部原文漏进了页面：%q", leak)
				}
			}
		})
	}
}

func TestWorkbenchPageErrorTextFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		workbenchenums.ErrPageNotFound: {"zh-CN": "页面不存在", "en-US": "Page not found"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	for _, tc := range []struct{ lang, want string }{
		{"zh-CN", "页面不存在"},
		{"en-US", "Page not found"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			stub := &workbenchPageStub{projectErr: errors.New("stub: 取不到")}
			router := newWorkbenchPageErrorRouter(t, New(stub, nil, nil, nil, nil, nil, nil))
			req := httptest.NewRequest(http.MethodGet, "/workbench?id=page-1", nil)
			req.Header.Set("Accept-Language", tc.lang)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			body := rec.Body.String()
			if rec.Code != http.StatusNotFound {
				t.Fatalf("状态码应为 404，得到 %d：%s", rec.Code, body)
			}

			assertWorkbenchErrorShell(t, rec, body, tc.want)
			if !strings.Contains(body, tc.want) {
				t.Errorf("%s 的失败文案应含 %q：%s", tc.lang, tc.want, body)
			}
			if strings.Contains(body, workbenchenums.ErrPageNotFound) {
				t.Errorf("页面上漏出了 i18n key %q", workbenchenums.ErrPageNotFound)
			}
		})
	}
}

func TestWorkbenchPageExitsVsPreviewEndpointShapes(t *testing.T) {

	code := stripLineCommentsForShape(readPackageGoSource(t))

	page := goFuncBodyForShape(t, code, "func (h *Handle) Workbench(c *gin.Context) {")
	if strings.Contains(page, "c.String(") {
		t.Error("工作台页面 handler 不得再有裸文本出口：它是整页 GET（编辑器壳），失败也必须回完整页")
	}
	for _, want := range []string{
		"renderWorkbenchPageError(c, http.StatusBadRequest, workbenchenums.ErrMissingPageID)",
		"renderWorkbenchPageError(c, http.StatusNotFound, workbenchenums.ErrPageNotFound)",
		"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrDraftEncodeFailed)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("整页失败出口缺 %q（三档都要走同一个渲染函数）", want)
		}
	}

	for _, sig := range []string{
		"func (h *Handle) Preview(c *gin.Context) {",
		"func (h *Handle) BlockPreview(c *gin.Context) {",
		"func (h *Handle) PreviewDraft(c *gin.Context) {",
	} {
		if !strings.Contains(goFuncBodyForShape(t, code, sig), "c.String(") {
			t.Errorf("%s 的裸文本出口被顺手改了：它不是整页编辑器外壳（预览文档 / 片段端点保持原形态）", sig)
		}
	}
}

func stripLineCommentsForShape(src string) string {
	var out strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

func goFuncBodyForShape(t *testing.T, src, signature string) string {
	t.Helper()
	start := strings.Index(src, signature)
	if start < 0 {
		t.Fatalf("在源码里找不到 %q（写法变了？这条守卫已失效）", signature)
	}
	rest := src[start:]
	open := strings.Index(rest, "{")
	if open < 0 {
		t.Fatalf("%q 后面没有函数体", signature)
	}
	depth := 0
	for i := open; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[open : i+1]
			}
		}
	}
	t.Fatalf("%q 的函数体没有配平（花括号不闭合）", signature)
	return ""
}

type workbenchBlockStub struct {
	blockcontract.BlockService
	block *blockdto.BlockResp
	err   error
}

func (s *workbenchBlockStub) List(_ context.Context, _ *blockdto.ListReq) ([]blockdto.BlockResp, error) {
	return nil, nil
}

func (s *workbenchBlockStub) Detail(_ context.Context, _ *blockdto.DetailReq) (*blockdto.BlockResp, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.block == nil {
		return nil, errors.New("stub: 块记录不存在")
	}
	return s.block, nil
}

type workbenchTemplateStub struct {
	contenttemplatecontract.ContentTemplateService
	tpl *contenttemplatedto.TemplateResp
	err error
}

func (s *workbenchTemplateStub) Get(_ context.Context, _ *contenttemplatedto.GetReq) (*contenttemplatedto.TemplateResp, error) {
	return s.answer()
}

func (s *workbenchTemplateStub) GetScoped(_ context.Context, _, _ string) (*contenttemplatedto.TemplateResp, error) {
	return s.answer()
}

func (s *workbenchTemplateStub) answer() (*contenttemplatedto.TemplateResp, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.tpl, nil
}

type workbenchTemplatePreviewStub struct{}

func (workbenchTemplatePreviewStub) PreviewInstance(context.Context, *presentationdto.PreviewInstanceReq) (*presentationdto.PreviewInstanceResp, error) {
	return &presentationdto.PreviewInstanceResp{}, nil
}

type workbenchInstanceStub struct {
	presentationcontract.PresentationService
	inst *presentationdto.InstanceResp
	err  error
}

func (s *workbenchInstanceStub) Get(_ context.Context, _ *presentationdto.GetReq) (*presentationdto.InstanceResp, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.inst == nil {
		return nil, errors.New("stub: 实例记录不存在")
	}
	return s.inst, nil
}

type workbenchModeDeps struct {
	pages            pagecontract.PageService
	blocks           blockcontract.BlockService
	contentTemplates contenttemplatecontract.ContentTemplateService
	templatePreview  TemplatePreviewPort
	instances        presentationcontract.PresentationService
}

func (d workbenchModeDeps) newHandle() *Handle {

	h := New(d.pages, nil, d.blocks, nil, nil, d.contentTemplates, d.templatePreview)
	h.SetInstanceOverrideDeps(d.instances)
	return h
}

type workbenchPluginStub struct {
	plugincontract.PluginService
	asm *plugincontract.Assembly
}

func (s *workbenchPluginStub) EnabledAssembly(_ context.Context) (*plugincontract.Assembly, error) {
	return s.asm, nil
}

func injectBrokenComponentSchema(t *testing.T) plugincontract.PluginService {
	t.Helper()
	return &workbenchPluginStub{asm: &plugincontract.Assembly{

		Specs: map[string]*source.PluginComponentSpec{"plugin.broken": {}},
		InspectorSchemas: map[string][]byte{
			"plugin.broken": []byte("{ 这不是 JSON"),
		},
	}}
}

func TestPluginInspectorSchemaMergeEncodeFailure(t *testing.T) {
	goodDoc := json.RawMessage(`{"type":"root"}`)
	pageOK := &pagedto.PageResp{ID: "page-1", ProjectID: "project-1", DraftDocument: goodDoc}

	router := newWorkbenchPageErrorRouter(t, New(&workbenchPageStub{page: pageOK}, nil,
		&workbenchBlockStub{}, injectBrokenComponentSchema(t), nil, nil, nil))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/workbench?id=page-1&lang=zh-CN", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码应为 500，得到 %d：%s", rec.Code, body)
	}
	assertWorkbenchErrorShell(t, rec, body, "组件 schema 序列化失败")
	if strings.Contains(body, "这不是 JSON") {
		t.Errorf("插件 schema 的原文漏到了页面上：%s", body)
	}
}

func TestWorkbenchModeErrorBranchesRenderFullShell(t *testing.T) {

	rawErr := errors.New(`pq: relation "blocks" does not exist`)
	goodDoc := json.RawMessage(`{"type":"root"}`)
	badJSON := json.RawMessage("{ 这不是 JSON")

	blockBad := &blockdto.BlockResp{ID: "block-1", ProjectID: "project-1", Name: "页眉", Kind: "header", Document: badJSON}
	tplOK := &contenttemplatedto.TemplateResp{ID: "tpl-1", Name: "商品卡", EntityType: "product", DraftDocument: goodDoc}
	tplBad := &contenttemplatedto.TemplateResp{ID: "tpl-1", Name: "商品卡", EntityType: "product", DraftDocument: badJSON}
	tplHeader := &contenttemplatedto.TemplateResp{ID: "tpl-2", Name: "页眉", EntityType: contenttemplatecontract.EntityTypeHeader, DraftDocument: goodDoc}
	instBad := &presentationdto.InstanceResp{ID: "inst-1", ProjectID: "project-1", EntityType: "product", EntityID: "42", Document: badJSON}
	instNoDoc := &presentationdto.InstanceResp{ID: "inst-1", ProjectID: "project-1", EntityType: "product", EntityID: "42"}

	const (
		blockURL = "/workbench?block=block-1&lang=zh-CN"
		tplURL   = "/workbench?template=tpl-1&entityType=product&entityId=42&lang=zh-CN"
		instURL  = "/workbench?instance=inst-1&lang=zh-CN"
	)

	cases := []struct {
		name       string
		deps       workbenchModeDeps
		url        string
		wantStatus int
		wantText   string
	}{
		{"块不存在：目标事实",
			workbenchModeDeps{blocks: &workbenchBlockStub{err: rawErr}}, blockURL,
			http.StatusNotFound, "全局块不存在"},
		{"块文档序列化失败：未知内部异常",
			workbenchModeDeps{blocks: &workbenchBlockStub{block: blockBad}}, blockURL,
			http.StatusInternalServerError, "块文档序列化失败"},
		{"模板编辑能力未装配：服务事实",
			workbenchModeDeps{}, "/workbench?template=tpl-1&lang=zh-CN",
			http.StatusServiceUnavailable, "内容模板编辑能力未装配"},
		{"模板不存在：目标事实",
			workbenchModeDeps{contentTemplates: &workbenchTemplateStub{err: rawErr}}, tplURL,
			http.StatusNotFound, "模板不存在"},

		{"普通模板缺样例实体：请求事实（{type} 替换）",
			workbenchModeDeps{contentTemplates: &workbenchTemplateStub{tpl: tplOK}, templatePreview: workbenchTemplatePreviewStub{}},
			"/workbench?template=tpl-1&entityType=product&lang=zh-CN",
			http.StatusBadRequest, "product"},
		{"模板预览能力未装配：服务事实",
			workbenchModeDeps{contentTemplates: &workbenchTemplateStub{tpl: tplOK}}, tplURL,
			http.StatusServiceUnavailable, "内容模板编辑能力未装配"},
		{"无实体模板预览能力未装配：服务事实",
			workbenchModeDeps{contentTemplates: &workbenchTemplateStub{tpl: tplHeader}}, "/workbench?template=tpl-2&lang=zh-CN",
			http.StatusServiceUnavailable, "无实体模板预览能力未装配"},
		{"模板文档序列化失败：未知内部异常",
			workbenchModeDeps{contentTemplates: &workbenchTemplateStub{tpl: tplBad}, templatePreview: workbenchTemplatePreviewStub{}}, tplURL,
			http.StatusInternalServerError, "模板文档序列化失败"},
		{"实例编辑能力未装配：服务事实",
			workbenchModeDeps{}, instURL,
			http.StatusServiceUnavailable, "实例编辑能力未装配"},
		{"实例不存在：目标事实",
			workbenchModeDeps{instances: &workbenchInstanceStub{err: rawErr}}, instURL,
			http.StatusNotFound, "实例不存在"},

		{"实例无可编辑文档：业务状态（facing 文案）",
			workbenchModeDeps{instances: &workbenchInstanceStub{inst: instNoDoc}}, instURL,
			http.StatusUnprocessableEntity, "这个实例还没有可编辑的文档"},
		{"实例文档序列化失败：未知内部异常",
			workbenchModeDeps{instances: &workbenchInstanceStub{inst: instBad}}, instURL,
			http.StatusInternalServerError, "草稿文档序列化失败"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newWorkbenchPageErrorRouter(t, tc.deps.newHandle())
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))

			body := rec.Body.String()
			if rec.Code != tc.wantStatus {
				t.Fatalf("状态码应为 %d，得到 %d：%s", tc.wantStatus, rec.Code, body)
			}
			assertWorkbenchErrorShell(t, rec, body, tc.wantText)

			for _, leak := range []string{
				rawErr.Error(), "这不是 JSON", "invalid character", "{type}",
				workbenchenums.ErrBlockNotFound, workbenchenums.ErrTemplateNotFound,
				workbenchenums.ErrInstanceNotFound, workbenchenums.ErrPreviewEntityIDRequired,
				workbenchenums.ErrComponentSchemaEncodeFailed,
			} {
				if strings.Contains(body, leak) {
					t.Errorf("页面上漏出了不该出现的原文 / key：%q", leak)
				}
			}
		})
	}
}

func TestComponentSchemaBuildFailureIsNotInducible(t *testing.T) {
	first, err := builder.ComponentSchemas()
	if err != nil {
		t.Fatalf("进程第一次构建组件 schema 就失败：%v（构建分支在请求路径上本就不可达，这里更无从覆盖）", err)
	}
	if len(first) == 0 {
		t.Fatal("组件 schema 为空：注册表没装配")
	}
}

func TestComponentSchemasReturnsIndependentCopy(t *testing.T) {
	const probe = "__independent_copy_probe__"

	first, err := builder.ComponentSchemas()
	if err != nil {
		t.Fatalf("取组件 schema 失败：%v", err)
	}
	first[probe] = json.RawMessage(`{"k":1}`)

	second, err := builder.ComponentSchemas()
	if err != nil {
		t.Fatalf("第二次取组件 schema 失败：%v", err)
	}
	if _, ok := second[probe]; ok {
		t.Error("改第一份返回值在第二次调用里看得见：返回的不是独立副本 —— " +
			"调用方合并自己的条目就是在写进程级缓存，并发画布请求会撞 map（DATA RACE）")
	}
	if reflect.ValueOf(first).Pointer() == reflect.ValueOf(second).Pointer() {
		t.Error("两次调用返回了同一张 map：缓存被按引用透传出去了")
	}

	if len(second) == 0 {
		t.Fatal("副本里没有内置组件 schema")
	}
}

func newWorkbenchErrorProbeRouter(t *testing.T, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.GET("/probe", handler)
	return router
}

func TestRenderWorkbenchPageErrorCoversModeKeys(t *testing.T) {
	keys := []string{
		workbenchenums.ErrBlockNotFound,
		workbenchenums.ErrBlockEncodeFailed,
		workbenchenums.ErrTemplateNotFound,
		workbenchenums.ErrTemplateEncodeFailed,
		workbenchenums.ErrEditorMetaEncodeFailed,
		workbenchenums.ErrComponentSchemaBuildFailed,
		workbenchenums.ErrComponentSchemaEncodeFailed,
		workbenchenums.ErrContentTemplateEditNotAssembled,
		workbenchenums.ErrStructureTemplatePreviewNotAssembled,
		workbenchenums.ErrInstanceEditNotAssembled,
		workbenchenums.ErrInstanceNotFound,
		workbenchenums.ErrDraftEncodeFailed,
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			router := newWorkbenchErrorProbeRouter(t, func(c *gin.Context) {
				renderWorkbenchPageError(c, http.StatusInternalServerError, key)
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe?lang=zh-CN", nil))

			body := rec.Body.String()
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("状态码应为 500，得到 %d：%s", rec.Code, body)
			}

			assertWorkbenchErrorShell(t, rec, body, "")
			if slot := strings.TrimSpace(errorMessageSlot(body)); slot == "" {
				t.Errorf("key %s 渲染出了空消息槽（词条缺失且兜底失效）：%s", key, body)
			}
			if strings.Contains(body, key) {
				t.Errorf("页面上漏出了 i18n key %q", key)
			}
		})
	}
}

func TestRenderWorkbenchPageErrorTextFallsBackOnEmptyCopy(t *testing.T) {
	router := newWorkbenchErrorProbeRouter(t, func(c *gin.Context) {
		renderWorkbenchPageErrorText(c, http.StatusInternalServerError, workbenchenums.ErrBlockEncodeFailed, "")
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe?lang=zh-CN", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码应为 500，得到 %d：%s", rec.Code, body)
	}
	assertWorkbenchErrorShell(t, rec, body, "块文档序列化失败")
}

func TestWorkbenchModeExitShapes(t *testing.T) {

	code := stripLineCommentsForShape(readPackageGoSource(t))

	modes := []struct {
		sig  string
		want []string
	}{
		{
			"func (h *Handle) workbenchBlock(c *gin.Context, blockID string) {",
			[]string{
				"renderWorkbenchPageError(c, http.StatusNotFound, workbenchenums.ErrBlockNotFound)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrBlockEncodeFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)",
			},
		},
		{
			"func (h *Handle) workbenchTemplate(c *gin.Context, templateID string) {",
			[]string{
				"renderWorkbenchPageError(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplateEditNotAssembled)",
				"renderWorkbenchPageError(c, http.StatusNotFound, workbenchenums.ErrTemplateNotFound)",
				"renderWorkbenchPageErrorText(c, http.StatusBadRequest, workbenchenums.ErrPreviewEntityIDRequired,",
				"renderWorkbenchPageError(c, http.StatusServiceUnavailable, workbenchenums.ErrStructureTemplatePreviewNotAssembled)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrTemplateEncodeFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)",
			},
		},
		{
			"func (h *Handle) workbenchInstance(c *gin.Context, instanceID string) {",
			[]string{
				"renderWorkbenchPageError(c, http.StatusServiceUnavailable, workbenchenums.ErrInstanceEditNotAssembled)",
				"renderWorkbenchPageError(c, http.StatusNotFound, workbenchenums.ErrInstanceNotFound)",
				"renderWorkbenchPageErrorText(c, http.StatusUnprocessableEntity, workbenchenums.ErrInstanceNoDocument,",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrDraftEncodeFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)",
				"renderWorkbenchPageError(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)",
			},
		},
	}

	for _, m := range modes {
		body := goFuncBodyForShape(t, code, m.sig)
		if strings.Contains(body, "c.String(") {
			t.Errorf("%s 不得再有裸文本出口：它渲染的是整页编辑器，失败也必须回完整页", m.sig)
		}
		for _, want := range m.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s 缺少整页失败出口 %q", m.sig, want)
			}
		}
	}

	for _, sig := range []string{
		"func (h *Handle) previewTemplateTarget(c *gin.Context, templateID, projectID string) (tpl *contenttemplatedto.TemplateResp, ok bool) {",
	} {
		if !strings.Contains(goFuncBodyForShape(t, code, sig), "c.String(") {
			t.Errorf("%s 的裸文本出口被顺手改了：它不是整页编辑器外壳", sig)
		}
	}
}

func readPackageGoSource(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读包目录失败：%v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(n)
		if err != nil {
			t.Fatalf("读 %s 失败：%v", n, err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	return b.String()
}
