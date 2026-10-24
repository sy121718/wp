package workbenchhttp

// workbench_page_error_test.go — 工作台**整页**失败出口的三档（400 / 404 / 500）。
//
// 被守的边界（每条都在别处不会红）：
//
//  1. **形态**：?id= 缺参 / 页面取不到 / 草稿序列化失败，三条都必须回**完整页壳**
//     （text/html + 页头页尾 + 「返回后台」一条回路 + no-store），而不是一行 text/plain。
//     裸文本把作者丢在一片空白上：地址栏里明明是一个页面 URL，页面上却没有回退路径。
//     契约见 docs/rules/template-boundary.md「整页 GET 的失败出口」。
//  2. **事实区分**：400（请求缺参）/ 404（目标不存在）/ 500（未知内部异常）的状态码与文案
//     各自不同 —— 合档会让「你少传了参数」和「服务端炸了」在页面上长得一样。
//  3. **文案归口 + 请求语言**：文案来自 workbenchShortFallbacks 的白名单短句、按请求语言取词；
//     内部原文（数据库错误、json.Marshal 的原文）只进日志、不进响应体。
//  4. **片段 / 预览端点不受影响**：Preview / BlockPreview / PreviewDraft 是 iframe 内嵌文档与
//     片段端点，不是编辑器外壳 —— 它们的 c.String 出口保持原形态（把编辑器 chrome 塞进预览
//     画布里是另一件事）。这条用源码形状钉住：改页面出口 ≠ 可以顺手改预览端点。
//
// 为什么用真实 Jet 渲染而不是断言 Go 侧数据：整页失败出口的价值**全在渲染出来的那具壳**。
// 只断言 `c.HTML(400, "workbench/error", …)` 的实参，模板里少写 </html> 也照样绿；而
// 「渲染中途截断」正是本契约要治的失败形态之一。所以这里走与线上开发模式同一条路径
// （templates.NewJetHTMLRender 读磁盘模板），并逐项检查页头 / 页尾 / 出口 / 文案。
//
// 为什么不复用 workbench/layout：那个外壳用点号取大量编辑器专用键（.jsVer / .document /
// .schemas / .isBlock …），错误请求下这些值都不存在 —— 复用等于把「页面打不开」换成又一个
// 渲染期 500。错误外壳因此是独立完整页 internal/templates/workbench/error.html。

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
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/builder/source"
	plugincontract "go_wp/internal/module/plugin/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

// workbenchPageStub 只实现 PageByID 真正走到的两个契约方法（ProjectOfPage → Detail）；
// 其余方法由嵌入的接口兜住（本测试不调用它们）。
type workbenchPageStub struct {
	pagecontract.PageService
	// projectErr 非空 → 「页面取不到」（ProjectOfPage 失败，Service.PageByID 直接返回错误）。
	projectErr error
	// page 非空 → 详情读到了；DraftDocument 给一段非法 JSON 即可造出「序列化失败」。
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

// newWorkbenchPageErrorRouter 装配与线上同一条渲染路径的测试路由（不依赖数据库）。
func newWorkbenchPageErrorRouter(t *testing.T, h *Handle) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.GET("/workbench", h.Workbench)
	return router
}

// assertWorkbenchErrorShell 整页失败出口的公共判据。
func assertWorkbenchErrorShell(t *testing.T, rec *httptest.ResponseRecorder, body, wantText string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("整页失败出口必须是 HTML（Content-Type 含 text/html），得到 %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("失败页不该被缓存（Cache-Control 含 no-store），得到 %q", cc)
	}
	// 完整壳：只看 <!DOCTYPE 会漏掉「渲染到一半截断」——尾标记与外壳 chrome 一起看。
	// href="/admin"：失败页唯一需要的动作是「回到一个能用的地方」。
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
	// 文案必须落在**消息槽**里，而不是「页面上某处出现过」：标题槽带的是同一句话，
	// 只扫整页正文时，把消息槽写死成中文（不随请求语言变）也会照样绿。
	slot := errorMessageSlot(body)
	if slot == "" {
		t.Errorf("没有找到消息槽（模板结构变了？）：%s", body)
	} else if !strings.Contains(slot, wantText) {
		t.Errorf("消息槽里的文案应为 %q，实际 %q", wantText, slot)
	}
}

// errorMessageSlot 取消息槽（id="wb-error-message" 那个元素）的正文。
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

// TestWorkbenchPageErrorRendersFullShellNotBareText 缺 ?id= 时回完整页（400），不是一行裸文本。
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

// TestWorkbenchPageErrorDistinguishesMissingParamPageGoneAndServerFault 三档事实互不混同。
//
// 这是本契约最容易退化的地方：三处出口共用同一个渲染函数，只要有人把状态码写成常量、
// 或把文案统一成「页面打不开」，三条就会重新变成一条 —— 而那条恰好是任务要治的形态。
func TestWorkbenchPageErrorDistinguishesMissingParamPageGoneAndServerFault(t *testing.T) {
	// 桩错误刻意写成「像数据库原文」：它只许出现在日志里。
	rawErr := errors.New(`pq: relation "pages" does not exist`)
	// DraftDocument 是 json.RawMessage：一段非法 JSON 就能让 json.Marshal 失败，
	// 于是 500 这条在**不依赖数据库**的前提下可达（此前没有测试碰过它）。
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
			// 内部原文只进日志。
			for _, leak := range []string{rawErr.Error(), "这不是 JSON", "invalid character"} {
				if strings.Contains(body, leak) {
					t.Errorf("内部原文漏进了页面：%q", leak)
				}
			}
		})
	}
}

// TestWorkbenchPageErrorTextFollowsRequestLanguage 失败文案随请求语言取词，而不是写死中文。
//
// 词条真源：public/migrations/452_i18n_workbench_admin_plugin_fragment.sql:22-23（同表
// :20-21 是缺参、:56-57 是序列化失败）。这里注入测试值而不是读迁移：断言的是「按语言取词」
// 这条链路，词条本身的 seed 由门禁 check-i18n-keys-seeded.sh 与迁移测试守。
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
			// 同一套整页判据（含「文案落在消息槽里」）：否则把消息槽写死成中文也能绿。
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

// TestWorkbenchPageExitsVsPreviewEndpointShapes 页面出口与预览/片段端点的形状分界。
//
// 前半段钉「页面 handler 不再有裸文本出口、三档都走整页渲染函数」；后半段钉**反面**：
// Preview / BlockPreview / PreviewDraft 的 c.String 保持原样 —— 它们是 iframe 内嵌的完整
// 文档与片段端点，不是编辑器外壳（把工作台 topbar / inspector 塞进预览画布里会同时破坏
// 预览内容与编辑器布局）。没有后半段，下一个人「顺手统一」就会把它们一起改掉。
func TestWorkbenchPageExitsVsPreviewEndpointShapes(t *testing.T) {
	raw, err := os.ReadFile("workbench_page.go")
	if err != nil {
		t.Fatalf("读 workbench_page.go 失败：%v", err)
	}
	code := stripLineCommentsForShape(string(raw))

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

// stripLineCommentsForShape 剥掉行尾与整行注释：注释里解释「旧写法为什么是缺陷」时会提
// 旧形态，那不算源码形状（同 workbench_error_shape_test.go 的取舍）。
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

// goFuncBodyForShape 取一个函数的函数体（按花括号配平）。签名必须在 src 里唯一。
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

// ─────────────────────────────────────────────────────────────────────────────
// 三种画布模式（块 / 模板 / 实例）的失败出口：18 处裸文本出口改完整页后的逐档守卫。
//
// 它们和页面模式同一个问题：地址栏里是**整页编辑器**的 URL（?block= / ?template= / ?instance=），
// 失败时回 text/plain 等于把作者丢在一片空白上 —— 没有页头、没有「返回后台」、没有任何原因。
// 契约见 docs/rules/template-boundary.md「整页 GET 的失败出口」。
//
// 用例按**分支**而不是按 handler 切：三档（400 / 404 / 422 / 500 / 503）的状态码与文案必须
// 各自不同，合成一条会让「你少传了样例实体」和「服务端炸了」在页面上长得一样 —— 那正是
// 这批出口原先的病（一溜 c.String）。
//
// 两条分支在进程内不可端到端注入（组件 schema **生成**失败、三处 meta 序列化失败）：
// 见 TestComponentSchemaBuildFailureIsNotInducible（机制证据）与
// TestRenderWorkbenchPageErrorCoversModeKeys（key → 完整壳的渲染覆盖）；分支体本身由
// TestWorkbenchModeExitShapes 的源码形状守。
// ─────────────────────────────────────────────────────────────────────────────

// workbenchBlockStub 只实现块画布走到的契约方法（Service.BlockByID → blocks.Detail）。
type workbenchBlockStub struct {
	blockcontract.BlockService
	block *blockdto.BlockResp
	err   error
}

// List 供 workbenchPage 的 palette 取全局块摘要（没有任何块时返回空列表即可）。
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

// workbenchTemplateStub 模板画布：Get（无工程作用域）与 GetScoped（带 projectId）同源。
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

// workbenchTemplatePreviewStub 模板画布的预览端口：本测试只用「已装配」这一事实
// （它是 503「未装配」那档的反面，决定请求能否继续往里走）。
type workbenchTemplatePreviewStub struct{}

func (workbenchTemplatePreviewStub) PreviewInstance(context.Context, *presentationdto.PreviewInstanceReq) (*presentationdto.PreviewInstanceResp, error) {
	return &presentationdto.PreviewInstanceResp{}, nil
}

// workbenchInstanceStub 实例画布：走 h.instances.Get（经 SetInstanceOverrideDeps 注入）。
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

// workbenchModeDeps 一个用例只需要一两种能力：没给的那几个端口留 nil ——
// nil 恰好就是「能力未装配」那档（503）的构造方式。
type workbenchModeDeps struct {
	pages            pagecontract.PageService
	blocks           blockcontract.BlockService
	contentTemplates contenttemplatecontract.ContentTemplateService
	templatePreview  TemplatePreviewPort
	instances        presentationcontract.PresentationService
	// plugins 插件契约（只有「组件 schema 序列化失败」一档需要它：
	// 非法 JSON 挂在插件装配素材上才能进序列化分支，见 injectBrokenComponentSchema）。
	plugins plugincontract.PluginService
}

func (d workbenchModeDeps) newHandle() *Handle {
	h := New(d.pages, nil, d.blocks, d.plugins, nil, d.contentTemplates, d.templatePreview)
	h.SetInstanceOverrideDeps(d.instances)
	return h
}

// workbenchPluginStub 只提供 EnabledAssembly（本包只消费这一个方法）；
// 其余方法由嵌入的接口兜住（本测试不调用它们）。
//
// 能力未装配用 nil 接口表达，不用「返回 nil 的桩」：本包两者走同一条降级分支。
type workbenchPluginStub struct {
	plugincontract.PluginService
	asm *plugincontract.Assembly
}

func (s *workbenchPluginStub) EnabledAssembly(_ context.Context) (*plugincontract.Assembly, error) {
	return s.asm, nil
}

// injectBrokenComponentSchema 让 workbenchPage 的 json.Marshal(schemas) 必然失败。
//
// 手法：非法 JSON 挂在**插件装配素材**上（plugincontract.Assembly.InspectorSchemas），
// 由 workbenchPage 把它们并进它自己那份 schema 表。这是请求内真实存在的唯一入口 ——
// 另外三种模式（块 / 模板 / 实例）不合并插件 schema，走不到这条分支。
//
// 为什么不再往 builder.ComponentSchemas() 返回的 map 里塞：那个函数自从改成
// **每次返回进程级缓存的副本**（并发安全）之后，改返回值就不再能影响下一次调用 ——
// 这本身就是那条修复要保证的性质，所以注入手法必须换到请求入参上，而不是和它对抗。
// 附带好处：不再需要 t.Cleanup 清理进程级缓存（旧手法漏清会让同包其它测试跟着红）。
func injectBrokenComponentSchema(t *testing.T) plugincontract.PluginService {
	t.Helper()
	return &workbenchPluginStub{asm: &plugincontract.Assembly{
		// Specs 必须非空：Service.PluginAssembly 用 len(PluginFS)>0 || len(Specs)>0
		// 判「这个装配素材是不是空的」，只带 InspectorSchemas 会被整个丢掉、返回 nil。
		Specs: map[string]*source.PluginComponentSpec{"plugin.broken": {}},
		InspectorSchemas: map[string][]byte{
			"plugin.broken": []byte("{ 这不是 JSON"),
		},
	}}
}

// TestPluginInspectorSchemaMergeEncodeFailure 插件 schema 并入后无法序列化 → 500「组件 schema
// 序列化失败」。
//
// 这条是**唯一**能走到该分支的路径：workbenchPage 把插件 InspectorSchemas 并进自己那份
// ComponentSchemas() 副本，只要插件给的不是合法 JSON，合并后的 Marshal 就失败。
// 同时它替另一件事兜底：ComponentSchemas() 现在返回的是可写副本 —— 若哪天又变回
// 「按引用透传全局缓存」，这条用例仍会绿（合并照样发生），但并发会撞 map，由
// TestComponentSchemasReturnsIndependentCopy 钉住。
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

// TestWorkbenchModeErrorBranchesRenderFullShell 三种模式的每一档失败都回完整错误壳。
func TestWorkbenchModeErrorBranchesRenderFullShell(t *testing.T) {
	// 桩错误刻意写成「像数据库原文」：它只许出现在日志里。
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

	// 为什么这里没有「组件 schema 序列化失败」这一档：块 / 模板 / 实例三种模式**根本不合并
	// 插件 schema**（只有 workbenchPage 合并，见 workbench_page.go 的 asm.InspectorSchemas 循环），
	// 所以它们的 schemas 表恒定是内置组件、恒可序列化 —— 这个分支对它们不存在。
	// 此前这三条能绿，靠的正是「ComponentSchemas() 按引用返回全局缓存」这个缺陷：注入会把坏
	// 数据写进进程级缓存，于是四种模式一起读到。缓存改返回副本（并发安全）后，这条路自然断开。
	// 合并路径本身由 TestPluginInspectorSchemaMergeEncodeFailure 单独覆盖。
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
		// {type} 由词条携带、在渲染出口处替换：取词与替换的顺序错了，这里会漏出 "{type}"。
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
		// 422 不是「服务端炸了」：这个实例只是还没有可编辑的文档（业务状态），
		// 文案走 workbenchFacingFallbacks 那条受控出口，不是短句表。
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
			// 内部原文只进日志；i18n key 与未替换的占位符都不许出现在页面上。
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

// TestComponentSchemaBuildFailureIsNotInducible 「组件 schema **生成**失败」这条 500 在进程内
// 不可注入 —— 这里给出机制证据，而不是一句「测不了」。
//
// 机制：builder.ComponentSchemas() 用 sync.Once 建缓存，构建只发生在**进程第一次调用**时；
// 构建失败的唯一来源是 core.SchemaJSON(sp.PropsSpec()) 在固定注册表上失败，而注册表在包
// init 期就定死了，请求侧没有任何输入能影响它。
func TestComponentSchemaBuildFailureIsNotInducible(t *testing.T) {
	first, err := builder.ComponentSchemas()
	if err != nil {
		t.Fatalf("进程第一次构建组件 schema 就失败：%v（构建分支在请求路径上本就不可达，这里更无从覆盖）", err)
	}
	if len(first) == 0 {
		t.Fatal("组件 schema 为空：注册表没装配")
	}
}

// TestComponentSchemasReturnsIndependentCopy 每次调用拿到的是**独立副本**。
//
// 这条守的是并发安全本身，而不是某个分支覆盖率：ComponentSchemas() 返回的是进程级缓存的
// 浅拷贝，调用方（workbenchPage 把插件 schema 并进同一张表）改它**不能**影响下一次调用，
// 也不能与并发请求撞写。此前的写法规避了拷贝，于是任一请求往里合并插件 schema 就等于
// 写全局缓存 —— 两个画布请求同时到达就是 DATA RACE（go test -race 会报）。
//
// 三条断言各自钉住一个性质：副本可写、写后不影响缓存、两次调用互不相干。
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
	// 内置条目必须在两份里都在（副本不能是空的）。
	if len(second) == 0 {
		t.Fatal("副本里没有内置组件 schema")
	}
}

// newWorkbenchErrorProbeRouter 直接调渲染函数的探针路由：只覆盖「key → 完整壳」这一段。
func newWorkbenchErrorProbeRouter(t *testing.T, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.GET("/probe", handler)
	return router
}

// TestRenderWorkbenchPageErrorCoversModeKeys 三种模式用到的每个失败 key 都能渲染出完整壳。
//
// **这条不触发失败分支**：只覆盖「key → 完整壳 + 非空归口文案」这一段渲染映射，报告里按渲染映射计、不按失败分支覆盖计；分支体是否还在由 TestWorkbenchModeExitShapes 的源码形状守。
// 为什么单独钉这一段：三种模式里有两条分支在进程内无法端到端触发 —— 组件 schema **生成**失败
// （上一条测试给了机制证据）与三处 meta 序列化失败（meta 的字段全是本进程算出来的 string/int，
// 没有请求侧能塞进去的 RawMessage，json.Marshal 不会失败）。它们的分支体就是一行
// renderWorkbenchPageError(...)，所以这里按 key 把「key → 完整壳 + 非空归口文案」钉住，
// 分支体本身由 TestWorkbenchModeExitShapes 的源码形状守。
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
			// wantText 传空：每个 key 的归口文案在短句表里，这里钉的是「有文案且落在消息槽」。
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

// TestRenderWorkbenchPageErrorTextFallsBackOnEmptyCopy 空的成品文案必须回落到短句表。
//
// 守的是新增的 text 变体：调用方先把文案算好再交给渲染（{type} 替换过的 / facing 白名单的）,
// 一旦上游白名单漏登记，传进来的就是空串。没有兜底时失败页只剩顶栏与一个返回按钮 ——
// 用户看不到任何原因，比裸文本好不了多少。
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

// TestWorkbenchModeExitShapes 三种模式的源码形状：18 处裸文本出口必须全部改走整页渲染。
//
// 与页面模式那条形状测试分开写：三个 handler 各有自己的三档组合（块 404+3×500；
// 模板 503×3+404+400+3×500；实例 503+404+422+500×3），逐条点名能在下一处
// 「顺手加个 c.String」时给出可读的失败信息。
func TestWorkbenchModeExitShapes(t *testing.T) {
	raw, err := os.ReadFile("workbench_page.go")
	if err != nil {
		t.Fatalf("读 workbench_page.go 失败：%v", err)
	}
	code := stripLineCommentsForShape(string(raw))

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

	// 反面：模板预览取目标与片段渲染不是整页外壳，c.String 保持原形态。
	for _, sig := range []string{
		"func (h *Handle) previewTemplateTarget(c *gin.Context, templateID, projectID string) (tpl *contenttemplatedto.TemplateResp, ok bool) {",
	} {
		if !strings.Contains(goFuncBodyForShape(t, code, sig), "c.String(") {
			t.Errorf("%s 的裸文本出口被顺手改了：它不是整页编辑器外壳", sig)
		}
	}
}
