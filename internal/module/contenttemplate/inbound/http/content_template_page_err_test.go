package contenttemplatehttp

// content_template_page_err_test.go — 工程列表装载失败时内容模板页必须降级渲染。
//
// 原先那一分支是 `c.String(500, adminui.MsgInternalError)`：浏览器里只有一块纯文本，
// 且渲染的是**未翻译的裸 key**（页面上直接显示 MsgInternalError 这串英文）—— 侧栏、页头、
// 筛选栏、列表全部消失。本用例把降级渲染的契约钉住：
//  1. HTTP 200 且是**完整页面**（响应体含 </html>，筛选控件与列表骨架在）；
//  2. 提示条是归口文案（当前语言译文 / 中文兜底），不是裸 key，也不含驱动原文；
//  3. 空态说的是「没读出来」，不是「还没有内容模板」；
//  4. **不能**说成「能力未装配」：那是 h.templates == nil 的语义，挂在一次取数失败上就是误报
//     （Ready 仍为 true 才谈得上「本页可用」）；
//  5. 引用面一并标成「查不出来」：工程都没读到，此时说「无引用」会让人以为可以放心删。

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
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

	"go_wp/internal/adminui"
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
)

// ctProjectListErrText 工程列表装载失败的「驱动原文」（照真实形态造：表名 + SQLSTATE）。
const ctProjectListErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

// ctFailingProjects 工程契约桩：List 恒失败。
type ctFailingProjects struct {
	projectcontract.ProjectService
}

func (ctFailingProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, errors.New(ctProjectListErrText)
}

// ctCalls 依赖调用计数。三个接口都有 List 方法，一个结构体实现不了三个接口，所以分三个桩共享这一份计数。
type ctCalls struct {
	impact  int
	list    int
	product int
	content int
}

// ctCountingTemplates 具备能力的模板契约桩（Impact 可用、清单返回空）：用来证明失败档**一次都没查**。
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

// ctCountingProducts / ctCountingContents：样例实体挑选会读的两张外部表（同一计数器）。
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

// TestContentTemplatesPageProjectListFailureRenders500 工程列表读不到 = **这一页的主数据失败**：
// 同页完整 HTML + 500 + no-store，且**列表主体一次都没查**。
//
// 判据是短路证据：这一支在 `Impact` / `templates.List` 之前 return，列表主体根本没读 ——
// 没有可靠工程归属的列表查询没有意义。2026-10 之前这里按「可选辅助降级」写成 200，
// 于是失败在监控里与正常页长得一样（`docs/rules/template-boundary.md` 的辅助示例同步纠正）。
func TestContentTemplatesPageProjectListFailureRenders500(t *testing.T) {
	rawErr := errors.New(ctProjectListErrText)
	calls := &ctCalls{}

	rec := renderContentTemplatesPageWithDeps(t, ctCountingTemplates{calls: calls}, ctFailingProjects{},
		ctCountingProducts{calls: calls}, ctCountingContents{calls: calls},
		"/admin/content-templates?entityType=header")
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("工程列表读不到（主数据失败）应为 500，实际 %d：%s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type 应含 text/html，得到 %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("失败页应带 no-store，得到 %q", cc)
	}
	// ① 完整页面：模板缺键会让 Jet 在那一行中断渲染（现象是 200/500 + 后面整块 HTML 消失），
	// 所以「页壳在不在」是这条契约的判据（internal/templates/CLAUDE.md）。
	for _, want := range []string{"</html>", `id="adminRail"`, `name="entityType"`, `role="alert"`, "内容模板"} {
		if !strings.Contains(body, want) {
			t.Errorf("失败页必须仍是这一页（缺 %q）—— 不能退回一块裸文本", want)
		}
	}
	// ② 筛选保留：当次的 entityType 仍然回显（失败不等于把用户的选择丢掉）。
	if !strings.Contains(body, `value="header" selected`) {
		t.Errorf("失败页应保留当次筛选 entityType=header：%s", body)
	}
	// ③ 归口提示：无 DB ⇒ 无词条 ⇒ 回落中文兜底；关键是**不是裸 key**。
	for _, want := range []string{
		"系统内部错误，请稍后重试",
		"列表没读出来",
		contentTemplateImpactLoadFailedFallback, // 引用面「没读到」也要说出来（且与「能力未装配」区分）
	} {
		if !strings.Contains(body, want) {
			t.Errorf("失败页面缺少受控提示 %q", want)
		}
	}
	for _, forbidden := range []string{
		"MsgInternalError",                // 裸 key（本批修掉的就是它）
		rawErr.Error(),                    // 驱动原文只进日志 —— 原样
		html.EscapeString(rawErr.Error()), // 以及 HTML 转义形态（Jet 把 `"` 渲染成 &#34;）
		"还没有内容模板",                         // 空态误导：不是「没有模板」，是没读出来
		"内容模板能力未装配，本页暂不可用。",               // 误报：能力装着呢，是这一次没读到工程
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("失败页面不应出现 %q", forbidden)
		}
	}
	// ④ 没有列表主体就没有行级动作：`<form action=".../bulk-delete">` 是包住整张表的页壳（常驻渲染，
	// 空表的勾选框点不出任何提交），真正的行级入口与批量动作条都挂在行上，所以只断言后两者缺席。
	for _, forbidden := range []string{
		"content-templates/activate", "content-templates/edit?id=", "批量删除",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("列表主体没读出来，不该渲染行级入口 %q：%s", forbidden, body)
		}
	}
	// ⑤ 短路：引用面 / 模板清单 / 样例实体查询一次都没发生（只看响应证明不了这件事）。
	if calls.impact != 0 || calls.list != 0 || calls.product != 0 || calls.content != 0 {
		t.Errorf("工程失败后不该再查引用面/清单/样例实体，实际 impact=%d list=%d product=%d content=%d",
			calls.impact, calls.list, calls.product, calls.content)
	}
	// ⑥ 日志这一端：原文必须完整落在本模块 scene 里（页面不泄 ≠ 丢弃）。
	if !ctTestLogHasRawError(t, rawErr) {
		t.Errorf("日志里没有 error=%q 的结构化记录 —— 归口变成了丢弃", rawErr.Error())
	}
}

// ctTemplateListErrText 模板清单装载失败的「驱动原文」（照真实形态造：表名 + SQLSTATE）。
const ctTemplateListErrText = `pq: relation "content_templates" does not exist (SQLSTATE 42P01)`

// ctOKProjects 工程契约桩：List 正常返回一个工程，把请求推进到主数据（模板清单）那一步。
type ctOKProjects struct {
	projectcontract.ProjectService
}

func (ctOKProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return []projectcontract.ProjectResp{{ID: "project-1", Name: "默认站点"}}, nil
}

// ctFailingTemplates 模板契约桩：引用面（Impact）可用，清单（List）恒失败。
//
// 两个都实现是刻意的：handler 在列表之前先做引用反查，只桩 List 会让 Impact 落到嵌入的
// nil 接口上（panic），测出来的就不是这条失败出口了。
type ctFailingTemplates struct {
	contenttemplatecontract.ContentTemplateService
	// err 指定本次 List 抛出的错误；留空用默认的驱动原文（ctTemplateListErrText）。
	// 日志正断言要拿「本次请求这一条」去比对，所以错误得能换。
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

// TestContentTemplatesPageMainListFailureRenders500 主数据（模板清单）读不到：
// 同页完整 HTML 500 + no-store，而不是一段 JSON，也不是 200 的「还没有模板」。
//
// 与 `projects.List` 失败那条（本文件 `:106` 的 TestContentTemplatesPageProjectListFailureRenders500）
// 的分界不在状态码 —— 两条都是 500 + no-store —— 而在失败时是否已经读完页面赖以成立的主数据；
// 两条分支的失败键都叫 LoadFailed，键本身不携带档位。
func TestContentTemplatesPageMainListFailureRenders500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handle := NewContentTemplatePageHandle(ctFailingTemplates{}, ctOKProjects{}, nil, nil)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/content-templates",
		adminui.PageSlot(ContentTemplatesPageTemplate),
		handle.ContentTemplatesPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/content-templates", nil))
	body := rec.Body.String()

	// ① 形态：500 + text/html + no-store
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据读不到应为 500，实际 %d：%s", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type 应含 text/html，得到 %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("失败页应带 no-store，得到 %q", cc)
	}
	// ② 仍是这一页：完整页壳 + 筛选骨架 + 受控提示（失败优先于空态）
	for _, want := range []string{
		"</body>", "</html>", `id="adminRail"`,
		`name="entityType"`, "内容模板",
		"系统内部错误，请稍后重试", "列表没读出来",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("失败页缺少 %q（页壳 / 骨架 / 受控提示）", want)
		}
	}
	// ③ 不泄原文 / 裸 key，也不把「没读出来」说成「还没有模板」
	for _, forbidden := range []string{
		"MsgInternalError", "还没有内容模板",
		"SQLSTATE", "42P01", "content_templates\"",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("失败页不应出现 %q", forbidden)
		}
	}
}

// ctTemplatesStub 模板契约桩：引用面可用，清单返回给定内容（nil = 空列表）。
//
// 清单里用**结构模板**（header）：非结构类型会触发样例实体挑选（sampleEntityID），
// 那一步依赖 product / content 契约，与本组用例要守的「列表渲染与否」无关。
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

// ctPageTestLogDirs 记录每个用例自己的日志目录（t.TempDir() 下）。
//
// logger 是**包级全局**配置（pkg/logger/manager.go:57-64 的 cfg/inited + 场景 logger 缓存）：
// 本文件用例串行执行（无 t.Parallel()），每次装配都把 logger.Init 指到本用例的目录，
// 日志断言因此只看得见本次请求写下的行。此前这一页的用例根本不 Init，行会落到默认
// 目录里 —— 既可能被上次运行的行喂出假通过，也和别的包互相踩。
var ctPageTestLogDirs sync.Map

func renderContentTemplatesPage(t *testing.T, svc contenttemplatecontract.ContentTemplateService) *httptest.ResponseRecorder {
	t.Helper()
	return renderContentTemplatesPageLang(t, svc, "")
}

// renderContentTemplatesPageLang 同上，可带 `?lang=` 语言覆盖
// （pkg/response 的协商顺序：cookie → query lang → Accept-Language）。
// renderContentTemplatesPageWithDeps 装配真实路由：真实 Jet 模板 + 页壳 + 本用例独占的日志目录；
// 四个依赖全部由调用方给 —— 失败档与成功档共用同一份装配，否则失败路径的断言会悄悄退化成「测了另一套中间件」。
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
	// 收尾关掉全局 logger：句柄留在已删除的临时目录上，后续用例若忘了 Init 就会写进 gone 目录。
	t.Cleanup(func() { _ = logger.Close() })

	handle := NewContentTemplatePageHandle(templatesSvc, projects, products, contents)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/content-templates",
		adminui.PageSlot(ContentTemplatesPageTemplate),
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

// TestContentTemplatesPageSuccessRendersList 成功有数据：200 的正常页面，不该出现任何失败痕迹。
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

// TestContentTemplatesPageEmptyRendersEmptyState 成功但为空：空态说「还没有模板」，
// 与「没读出来」（LoadFailed 分支）必须长得不一样。
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
	// 与主失败档（500 + no-store）的分界：真读到了、只是没有行，是 200 的正常页面。
	if cc := rec.Header().Get("Cache-Control"); strings.Contains(cc, "no-store") {
		t.Errorf("成功空态不该带 no-store（那是失败页的缓存语义），得到 %q", cc)
	}
}

// ctInternalErrorKey 归口文案的 i18n key（值就是 shell 的 MsgInternalError 常量值）。
//
// 这里写字面量而不引 shell 包标识符：check-shell-migration.sh 的 test 桶基线只许下降，
// 测试里每多一个 shell.* 都是一处使用点。
const ctInternalErrorKey = "MsgInternalError"

// TestContentTemplatesPageMainListFailureRendersEnglishWhenTermPresent 词条在场时失败页说英文。
//
// 归口文案那条链是「按请求语言取词」；这一条把「不是写死中文」钉住：注入 en 词条后页面必须给
// 译文，中文兜底与裸 key 都不该出现。
func TestContentTemplatesPageMainListFailureRendersEnglishWhenTermPresent(t *testing.T) {
	// InjectForTest 的形状是 key → lang → 译文（与资源文件同构），不是 lang → key。
	i18n.InjectForTest(map[string]map[string]string{
		ctInternalErrorKey: {"en-US": "EN-INTERNAL-ERROR"},
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
		contentTemplateErrInternalFallback, ctInternalErrorKey,
		ctTemplateListErrText, html.EscapeString(ctTemplateListErrText),
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("英文请求不应出现 %q", forbidden)
		}
	}
}

// TestContentTemplatesPageMainListFailureFallsBackWhenTermMissing 缺词条（无 DB 的测试环境就是这个
// 状态）：落中文兜底，而不是裸 key、也不是把驱动原文摆上去。
func TestContentTemplatesPageMainListFailureFallsBackWhenTermMissing(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rec := renderContentTemplatesPageLang(t, ctFailingTemplates{}, "en-US")
	body := rec.Body.String()

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主列表读不到应为 500，实际 %d：%s", rec.Code, body)
	}
	if !strings.Contains(body, contentTemplateErrInternalFallback) {
		t.Errorf("缺词条时应落中文兜底 %q：%s", contentTemplateErrInternalFallback, body)
	}
	for _, forbidden := range []string{ctInternalErrorKey, ctTemplateListErrText, html.EscapeString(ctTemplateListErrText)} {
		if strings.Contains(body, forbidden) {
			t.Errorf("缺词条时不应出现 %q", forbidden)
		}
	}
}

// TestContentTemplatesPageLoadFailureKeepsRawErrorInLog 「归口」不是「丢弃」的正面证据。
//
// 上面的失败用例只证明了「原文没进页面」（反面证据）。这一条查另一端：内部错误必须能在
// **本次请求**写下的结构化日志里被完整找回 —— 否则 contentTemplateInternalText 注释许诺的
// 「谁在哪个页面撞上了哪条 SQL / 约束」就永远没人能回答。
func TestContentTemplatesPageLoadFailureKeepsRawErrorInLog(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	rawErr := errors.New(`pq: relation "content_templates" does not exist (SQLSTATE 42P01)`)
	rec := renderContentTemplatesPage(t, ctFailingTemplates{err: rawErr})

	// 页面这一端：原样与 HTML 转义两种形态都不许出现（Jet 把 `"` 渲染成 &#34;）。
	body := rec.Body.String()
	for _, leaked := range []string{rawErr.Error(), html.EscapeString(rawErr.Error())} {
		if strings.Contains(body, leaked) {
			t.Fatalf("归口失败：原文进了页面：%s", body)
		}
	}
	// 日志这一端：本模块 scene 目录里必须有一条结构化记录的 error 字段**完整等于**原文。
	if !ctTestLogHasRawError(t, rawErr) {
		t.Errorf("日志里没有 error=%q 的结构化记录 —— 归口变成了丢弃", rawErr.Error())
	}
	// 自检：换一条本次没发生过的原文必须判为未命中 —— 否则「找得到」可能来自任何一行日志。
	if ctTestLogHasRawError(t, errors.New(`pq: relation "never_happened_ct" does not exist`)) {
		t.Errorf("自检失败：本次没发生过的原文也被判定为命中")
	}
}

// ctTestLogDir 取回本用例的日志目录；取不到说明断言落在没走 renderContentTemplatesPage* 的用例里。
func ctTestLogDir(t *testing.T) string {
	t.Helper()
	value, ok := ctPageTestLogDirs.Load(t.Name())
	if !ok {
		t.Fatalf("用例 %s 尚未装配测试路由，日志目录未知", t.Name())
	}
	return value.(string)
}

// ctTestLogHasRawError 在本用例专属日志目录里逐行解析结构化记录，判断有没有一条把原始错误
// **完整**留在 error 字段里。
//
//	· 解析 JSON 后比对 error 字段的完整值，而不是在整段文本里找子串 —— 子串会把「转义之后照样
//	  看得见」（`\"` → `"`）也算命中，那是另一种形式的没写进去；
//	· 目录来自 t.TempDir()（见 ctPageTestLogDirs），本用例之外的行进不来；
//	· 只认本模块的 scene 子目录（logger 按 scene 分目录，pkg/logger/core.go:33）：落到别的
//	  scene 里说明记录位置漂移了。
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
