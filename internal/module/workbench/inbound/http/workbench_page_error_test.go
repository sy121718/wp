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
	"go_wp/pkg/builder/source"
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
	"go_wp/pkg/response"
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

// assertWorkbenchJSONError 断言失败出口是 JSON，并返回 message 供调用方续断业务文案。
//
// 页面 GET 失败与所有非页面请求都走这一形状（AGENTS：没有失败页），所以这里把
// Content-Type 一起钉住 —— 防止有人又退回「渲染整页错误壳」的旧形状。
func assertWorkbenchJSONError(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantText string) string {
	t.Helper()

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("失败出口必须是 JSON（Content-Type 含 application/json），得到 %q：%s", ct, rec.Body.String())
	}

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("失败响应不是合法 JSON：%v，实际：%s", err, rec.Body.String())
	}
	if rec.Code != wantCode || body.Code != wantCode {
		t.Fatalf("状态码应为 %d，得到 HTTP %d / code %d：%s", wantCode, rec.Code, body.Code, rec.Body.String())
	}
	if strings.TrimSpace(body.Message) == "" {
		t.Fatalf("message 不能为空：%s", rec.Body.String())
	}
	if wantText != "" && !strings.Contains(body.Message, wantText) {
		t.Fatalf("message 应含 %q，实际 %q", wantText, body.Message)
	}
	return body.Message
}

func TestWorkbenchPageErrorExitsAsJSONNotShell(t *testing.T) {
	// 词库注入值照生产 seed 抄：取词收口后 message 归词库，不注入就会落到裸 key。
	i18n.InjectForTest(map[string]map[string]string{
		workbenchenums.ErrMissingPageID: {"zh-CN": "缺少页面 id"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	router := newWorkbenchPageErrorRouter(t, New(&workbenchPageStub{}, nil, nil, nil, nil, nil, nil))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/workbench?lang=zh-CN", nil))

	msg := assertWorkbenchJSONError(t, rec, http.StatusBadRequest, "缺少页面 id")
	if strings.Contains(msg, workbenchenums.ErrMissingPageID) {
		t.Errorf("message 漏出了 i18n key：%q", msg)
	}
}

func TestWorkbenchPageErrorDistinguishesMissingParamPageGoneAndServerFault(t *testing.T) {

	i18n.InjectForTest(map[string]map[string]string{
		workbenchenums.ErrMissingPageID:     {"zh-CN": "缺少页面 id"},
		workbenchenums.ErrPageNotFound:      {"zh-CN": "页面不存在"},
		workbenchenums.ErrDraftEncodeFailed: {"zh-CN": "草稿文档序列化失败"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

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

			msg := assertWorkbenchJSONError(t, rec, tc.wantStatus, tc.wantText)
			for _, other := range allTexts {
				if other != tc.wantText && strings.Contains(msg, other) {
					t.Errorf("三档文案必须互不混同：本页（%s）出现了别档文案 %q", tc.wantText, other)
				}
			}

			for _, leak := range []string{rawErr.Error(), "这不是 JSON", "invalid character"} {
				if strings.Contains(msg, leak) {
					t.Errorf("内部原文漏进了响应体：%q", leak)
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

			msg := assertWorkbenchJSONError(t, rec, http.StatusNotFound, tc.want)
			if strings.Contains(msg, workbenchenums.ErrPageNotFound) {
				t.Errorf("%s 的 message 漏出了 i18n key：%q", tc.lang, msg)
			}
		})
	}
}

func TestWorkbenchPageExitsVsPreviewEndpointShapes(t *testing.T) {

	code := stripLineCommentsForShape(readPackageGoSource(t))

	page := goFuncBodyForShape(t, code, "func (h *Handle) Workbench(c *gin.Context) {")
	for _, banned := range []string{"c.String(", "c.HTML(", "renderWorkbenchPageError("} {
		if strings.Contains(page, banned) {
			t.Errorf("工作台页面 handler 不得有 %q：页面 GET 失败与所有非页面请求一律走 JSON 出口", banned)
		}
	}
	for _, want := range []string{
		"response.ErrorWithMessage(c, http.StatusBadRequest, workbenchenums.ErrMissingPageID)",
		"response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrPageNotFound)",
		"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrDraftEncodeFailed)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("整页失败出口缺 %q", want)
		}
	}

	// 预览成功出口是字节流 —— 整页改 JSON 不该把这两处 c.Data 顺手一起改掉。
	// 这是 AGENTS 里唯一给非页面请求留的例外（构建器预览直接写 body），必须守住。
	if n := strings.Count(code, "c.Data("); n < 2 {
		t.Errorf("预览成功出口应为字节流（c.Data），实际只找到 %d 处", n)
	}
	if strings.Contains(code, "c.HTML(") {
		t.Error("workbench 域不得再有 c.HTML：失败页已退役，页面失败也只回 JSON")
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

	i18n.InjectForTest(map[string]map[string]string{
		workbenchenums.ErrComponentSchemaEncodeFailed: {"zh-CN": "组件 schema 序列化失败"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	router := newWorkbenchPageErrorRouter(t, New(&workbenchPageStub{page: pageOK}, nil,
		&workbenchBlockStub{}, injectBrokenComponentSchema(t), nil, nil, nil))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/workbench?id=page-1&lang=zh-CN", nil))

	msg := assertWorkbenchJSONError(t, rec, http.StatusInternalServerError, "组件 schema 序列化失败")
	if strings.Contains(msg, "这不是 JSON") {
		t.Errorf("插件 schema 的原文漏进了响应体：%q", msg)
	}
}

func TestWorkbenchModeErrorBranchesRenderJSON(t *testing.T) {

	// 这一族用例断的是「响应体里给用户看的文案」。取词收口后 enums 只声明 key，
	// 成品句归词库，所以必须先把生产词库里的这些词条注入进来 —— 否则缺词会落到
	// 共享归口文案（系统内部错误），测的就不是本来的业务事实了。
	// 期望值一律照生产 seed 抄，不为了迁就代码改短改长：
	// 292 是 workbench.err.* 的首屏批次，viewmode 相关词条各自在自己的迁移里。
	i18n.InjectForTest(map[string]map[string]string{
		workbenchenums.ErrBlockNotFound:                        {"zh-CN": "全局块不存在"},
		workbenchenums.ErrBlockEncodeFailed:                    {"zh-CN": "块文档序列化失败"},
		workbenchenums.ErrTemplateEncodeFailed:                 {"zh-CN": "模板文档序列化失败"},
		workbenchenums.ErrDraftEncodeFailed:                    {"zh-CN": "草稿文档序列化失败"},
		workbenchenums.ErrPreviewEntityIDRequired:              {"zh-CN": "缺少 {type} 的样例实体 id"},
		workbenchenums.ErrTemplateNotFound:                     {"zh-CN": "模板不存在"},
		workbenchenums.ErrInstanceNotFound:                     {"zh-CN": "实例不存在"},
		workbenchenums.ErrInstanceNoDocument:                   {"zh-CN": "这个实例还没有可编辑的文档"},
		workbenchenums.ErrContentTemplateEditNotAssembled:      {"zh-CN": "内容模板编辑能力未装配"},
		workbenchenums.ErrInstanceEditNotAssembled:             {"zh-CN": "实例编辑能力未装配"},
		workbenchenums.ErrStructureTemplatePreviewNotAssembled: {"zh-CN": "无实体模板预览能力未装配"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

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

			msg := assertWorkbenchJSONError(t, rec, tc.wantStatus, tc.wantText)

			for _, leak := range []string{
				rawErr.Error(), "这不是 JSON", "invalid character", "{type}",
				workbenchenums.ErrBlockNotFound, workbenchenums.ErrTemplateNotFound,
				workbenchenums.ErrInstanceNotFound, workbenchenums.ErrPreviewEntityIDRequired,
				workbenchenums.ErrComponentSchemaEncodeFailed,
			} {
				if strings.Contains(msg, leak) {
					t.Errorf("响应体里漏出了不该出现的原文 / key：%q", leak)
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

func TestWorkbenchErrorKeysRenderJSONWithoutLeak(t *testing.T) {
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

	// 每个 key 给一条可辨识的译文：message 必须等于它而不是裸 key —— 这条比原先
	// 「渲染出非空消息槽」更硬，因为它同时证明「key 真的被词库换掉了」。
	dict := make(map[string]map[string]string, len(keys))
	for _, key := range keys {
		dict[key] = map[string]string{"zh-CN": "译文:" + key}
	}
	i18n.InjectForTest(dict, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			router := newWorkbenchErrorProbeRouter(t, func(c *gin.Context) {
				response.ErrorWithMessage(c, http.StatusInternalServerError, key)
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe?lang=zh-CN", nil))

			msg := assertWorkbenchJSONError(t, rec, http.StatusInternalServerError, "译文:")
			if msg == key {
				t.Errorf("message 是裸 key，说明取词没生效：%q", msg)
			}
			if msg != "译文:"+key {
				t.Errorf("message 应等于注入的译文，实际 %q", msg)
			}
		})
	}
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
				"response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrBlockNotFound)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrBlockEncodeFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)",
			},
		},
		{
			"func (h *Handle) workbenchTemplate(c *gin.Context, templateID string) {",
			[]string{
				"response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrContentTemplateEditNotAssembled)",
				"response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrTemplateNotFound)",
				"response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrStructureTemplatePreviewNotAssembled)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrTemplateEncodeFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)",
			},
		},
		{
			"func (h *Handle) workbenchInstance(c *gin.Context, instanceID string) {",
			[]string{
				"response.ErrorWithMessage(c, http.StatusServiceUnavailable, workbenchenums.ErrInstanceEditNotAssembled)",
				"response.ErrorWithMessage(c, http.StatusNotFound, workbenchenums.ErrInstanceNotFound)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrDraftEncodeFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrEditorMetaEncodeFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaBuildFailed)",
				"response.ErrorWithMessage(c, http.StatusInternalServerError, workbenchenums.ErrComponentSchemaEncodeFailed)",
			},
		},
	}

	for _, m := range modes {
		body := goFuncBodyForShape(t, code, m.sig)
		for _, banned := range []string{"c.String(", "c.HTML(", "renderWorkbenchPageError"} {
			if strings.Contains(body, banned) {
				t.Errorf("%s 不得有 %q：整页失败也只回 JSON", m.sig, banned)
			}
		}
		for _, want := range m.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s 缺少 JSON 失败出口 %q", m.sig, want)
			}
		}
	}

	for _, sig := range []string{
		"func (h *Handle) previewTemplateTarget(c *gin.Context, templateID, projectID string) (tpl *contenttemplatedto.TemplateResp, ok bool) {",
	} {
		body := goFuncBodyForShape(t, code, sig)
		for _, banned := range []string{"c.String(", "c.HTML("} {
			if strings.Contains(body, banned) {
				t.Errorf("%s 不得有 %q：失败只回 JSON", sig, banned)
			}
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
