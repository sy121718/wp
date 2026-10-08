package feature

// ai_page_test.go — AI 后台页面的真实渲染与 PRG 三态（评审 7）。
//
// 覆盖范围（写在前面，避免高估）：模板 + 数据键 + handler 的重定向/片段出口。
// **不含**鉴权链（SessionAuth / CSRF / Casbin）—— 那条链的装配断言属于三层中间件的既有测试，
// 这里不重复充当；本文件用直挂 handler 的方式跑，理由与 sysconfig 页面测试一致：
// 整页继承 layout.html，而 layout 的侧栏来自 shell.PermContextMiddleware 注入的导航树，
// 测试环境没有登录会话，走 HTTP 只会拿到「页面暂时无法显示」。
//
// 钉住三件事：
//  1. 模板与数据键对不上会在渲染期炸（缺键 → 整页中断，只得到半截 HTML）；
//  2. 成功提交渲染成功提示页且真的落库；
//  3. 校验失败渲染失败提示页（文案在响应体里，不再挂在 query 上）。

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/templates"
)

// newAIPageEnv 真 PG + 真种子 + 真 service，返回页面 handler。
func newAIPageEnv(t *testing.T) (*aihttp.PageHandle, *aiservice.Service, *gorm.DB) {
	t.Helper()
	svc, db := newAIProviderService(t)
	return aihttp.NewPageHandle(svc), svc, db
}

// serveAIPage 把请求交给页面 handler（真实 Jet 引擎）。
// assertAIJump 断言写动作渲染了提示页（200 + 成功/失败态 + 文案）。
func assertAIJump(t *testing.T, rec *httptest.ResponseRecorder, ok bool, msg string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("应为提示页 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	state := "err"
	if ok {
		state = "ok"
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="`+state+`"`) {
		t.Fatalf("提示页状态应为 %s，body=%s", state, body)
	}
	if msg != "" && !strings.Contains(body, msg) {
		t.Fatalf("提示页缺少文案 %q，body=%s", msg, body)
	}
}

func serveAIPage(t *testing.T, ph *aihttp.PageHandle, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/admin/ai/providers", ph.ProvidersPage)
	engine.POST("/admin/ai/providers/save", ph.ProviderSave)
	engine.POST("/admin/ai/providers/delete", ph.ProviderDelete)
	engine.POST("/admin/ai/providers/status", ph.ProviderStatus)
	engine.POST("/admin/ai/providers/models/candidates", ph.ModelsCandidates)
	engine.POST("/admin/ai/providers/models/append", ph.ModelsAppend)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// renderAITemplate 用与 handler 同形的数据直渲一个模板，返回整页字节。
func renderAITemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := templates.NewJetHTMLRender("../../../../internal/templates", true).Instance(name, data).Render(rec); err != nil {
		t.Fatalf("模板 %s 渲染失败：%v", name, err)
	}
	return rec.Body.String()
}

// pageBaseData 页面模板的公共数据键（与 shell.Prepare 注入同形）。
func pageBaseData(title string) gin.H {
	return gin.H{
		"title":      title,
		"csrf_token": "test-token",
		"t":          func(key, fallback string) string { return fallback },
		// 页面测试要能渲出全部按钮，故把所有 AI 权限点都置 true（一码一路由，见 internal/permission/codes.go）。
		"Buttons": map[string]bool{
			"ai.provider_list": true, "ai.provider_get": true, "ai.provider_models_list": true,
			"ai.provider_save": true, "ai.provider_delete": true, "ai.provider_status": true,
			"ai.provider_models_save": true, "ai.provider_models_restore": true, "ai.provider_models_fetch": true,
			"ai.session_list": true, "ai.session_get": true, "ai.session_events": true,
			"ai.session_fold_plan": true, "ai.session_append": true, "ai.session_rename": true,
			"ai.session_archive": true, "ai.session_fold": true,
			// 会话页的发消息区按对话入口的权限点渲染（页面路由借 /api/ai/chat 的 casbin obj）。
			"ai.chat": true,
		},
		"Err":  "",
		"Done": "",
	}
}

// sessionPageData 会话页模板的完整数据键（与 aihttp.SessionPageHandle 的装配同形）。
//
// 会话页比别的页面多三组键：看板（Usage / Trend）、筛选候选（FilterOptions）、
// 模型标签（ProviderCards / Presets / BuiltinKeys）。模板按真实 handler 的输出写，
// 少一个键 Jet 就中断整页（.Filter 这种点号访问在 map 上缺键是硬错误，不是空串），
// 所以这里宁可摆一串零值，也不让用例去猜哪些键「反正用不到」。
func sessionPageData() gin.H {
	data := pageBaseData("AI 会话")
	data["Rows"] = []gin.H{}
	data["Total"] = 0
	data["Page"] = 1
	data["PageSize"] = 20
	data["TotalPages"] = 1
	data["HasPrev"] = false
	data["HasNext"] = false
	data["PrevURL"] = ""
	data["NextURL"] = ""
	data["Keyword"] = ""
	data["Status"] = -1
	data["Tab"] = "sessions"
	data["IsModels"] = false
	data["Filter"] = aidto.SessionQuery{Status: -1}
	data["Usage"] = aidto.SessionUsage{}
	data["Trend"] = aidto.SessionTrend{}
	// 标签级权限：两个都开，默认用例覆盖「两个标签都渲染」。
	data["CanViewModels"] = true
	data["CanViewSessions"] = true
	data["FilterOptions"] = aidto.SessionFilterOptions{Providers: []string{}, Models: []string{}, Creators: []int64{}}
	data["ProviderCards"] = []gin.H{}
	data["Providers"] = []aidto.Provider{}
	data["Presets"] = aiservice.BuiltinPresets()
	data["BuiltinKeys"] = aiservice.BuiltinProviderKeys()
	data["ProtocolOptions"] = aienums.ProtocolOptions
	return data
}

// TestAISessionsPageRendersUsageHovercard 列表行的 token 单元格给出按供应商/模型的拆分悬浮卡。
//
// 这条路径上有两处容易写坏：① 行数据是 {Session, Usage} 的嵌套结构，模板取错一层就是
// 运行时整页 500；② 拆分表在 <template> 里 —— Jet 若把它当普通节点渲染，
// 悬浮卡的内容会直接出现在表格单元格里（页面一打开就摊开）。两点都钉住。
func TestAISessionsPageRendersUsageHovercard(t *testing.T) {
	data := sessionPageData()
	data["Rows"] = []gin.H{{
		"Session": aidto.Session{
			ID: 3, SessionKey: "k-3", Title: "会话三", Status: 1,
			EventCount: 4, ContextTokens: 15,
		},
		"Usage": []aidto.SessionModelUsage{
			{ProviderKey: "opencode-go", ModelID: "muse-spark", Events: 2, Tokens: 12, TokensText: "12"},
			// 529 之前的历史事件：没有来源可记，展示层要单独成组，不能并进上面那家。
			{Unrecorded: true, Events: 2, Tokens: 3, TokensText: "3"},
		},
		// 流水段：聚合回答「钱花在哪家」，这一段回答「哪一次特别慢 / 哪一次失败了」。
		// 两条覆盖成功与失败两个分支 —— 失败那一格显示的是**归口后的原因**，
		// 不是裸 i18n key，也不是一句干巴巴的「失败」。
		"Calls": aidto.SessionCalls{
			Total: 7,
			Rows: []aidto.SessionCallRow{
				{Time: "10-04 14:12", ProviderKey: "opencode-go", ModelID: "muse-spark", LatencyText: "2.9s", Tokens: 465, TokensText: "465", OK: true},
				{Time: "10-04 13:40", ProviderKey: "opencode-go", ModelID: "muse-spark", LatencyText: "480ms", Tokens: 0, TokensText: "0", ErrorKey: aienums.ErrInternal, ErrorText: "服务器内部错误，请稍后重试"},
			},
		},
	}}
	body := renderAITemplate(t, "admin/ai/sessions", data)

	for _, want := range []string{
		`data-wb-hover`,
		`class="wb-hover-trigger"`,
		`<template class="wb-hover-panel">`,
		`opencode-go`,
		`muse-spark`,
		`未记录`,
		// 流水段：标题、总数（含单位）、以及每一次调用的耗时与状态。
		"最近调用",
		"共",
		"7",
		"次调用",
		"2.9s",
		"480ms",
		"成功",
		"服务器内部错误，请稍后重试",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("token 悬浮卡缺少 %q", want)
		}
	}
	// 失败原因必须已经翻译过：裸 i18n key 上页面等于把内部标识透给用户。
	if strings.Contains(body, aienums.ErrInternal) {
		t.Fatalf("悬浮卡里出现了未翻译的 i18n key：%s", aienums.ErrInternal)
	}
	if got := strings.Count(body, `class="wb-hover-panel"`); got != 1 {
		t.Fatalf("悬浮面板应恰好一个，实际 %d", got)
	}
	if strings.Contains(body, `{{`) {
		t.Fatal("渲染结果里残留未解析的模板标记")
	}
}

// TestAIModelsTabRenders 模型标签渲染完整、且有可用的新建入口。
//
// 两个页面合成一个之后（532）模型那块不再有独立模板：它就是 sessions.html 的第一个标签。
// 标签顺序也在这里钉住 —— 入口叫「大模型管理」，第一屏就该是模型配置。
func TestAIModelsTabRenders(t *testing.T) {
	data := sessionPageData()
	data["IsModels"] = true
	body := renderAITemplate(t, "admin/ai/sessions", data)

	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	for _, want := range []string{
		`action="/admin/ai/providers/save"`,
		`name="providerKey"`,
		`name="csrf_token"`,
		`value="test-token"`,
		"还没有配置任何 AI 供应商",
		// 双 tab 与预设下拉：预设 tab 的表单字段名同为 providerKey，缺了它「选一家填密钥」就无从下手。
		"第三方模型提供商",
		"自定义模型 API",
		"OpenAI",
		// 候选弹窗的宿主：片段由 htmx 换进这里。
		"id=\"ai-picker-host\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("模型标签缺少 %q", want)
		}
	}
	// 模型标签必须排在会话标签之前。
	mi := strings.Index(body, "id=\"ai-tab-models\"")
	si := strings.Index(body, "id=\"ai-tab-sessions\"")
	if mi < 0 || si < 0 || mi > si {
		t.Fatalf("标签顺序应为模型在前，实际 models=%d sessions=%d", mi, si)
	}
	if strings.Contains(body, `{{`) {
		t.Fatal("渲染结果里残留未解析的模板标记")
	}
}

// TestAIModelPickerFragmentRenders 候选弹窗作为**片段**渲染（含「已在目录」的 disable 分支）。
//
// 这条路径上最容易写错的是模板里对 map 的取值：`{{existing[id]}}` 与 `isset(...)`
// 只有真渲一次才知道语法对不对 —— 错了就是运行时 500。
//
// 页面合并之后（532）它不再靠「重渲染整页 + data-modal-auto-open」，而是被 htmx 换进
// #ai-picker-host：带整页壳的片段会把 <dialog> 埋进 body 里出不来，所以这里同时钉住「没有整页壳」。
func TestAIModelPickerFragmentRenders(t *testing.T) {
	data := pageBaseData("模型")
	data["Picker"] = gin.H{
		"Provider":   aidto.Provider{ID: 7, ProviderKey: "acme-gateway", DisplayName: "Acme Gateway", Version: 2},
		"Candidates": []string{"gpt-4o", "already-there"},
		"Existing":   map[string]bool{"already-there": true},
	}
	body := renderAITemplate(t, "admin/ai/provider_picker", data)
	if strings.Contains(body, "<html") || strings.Contains(body, "<!doctype") {
		t.Fatal("候选弹窗应是片段，不该带整页壳")
	}

	for _, want := range []string{
		"选择要添加的模型",
		"以下是模型提供商的可用模型",
		"搜索模型",
		"全选",
		"添加所选",
		`name="modelIds"`,
		`value="gpt-4o"`,
		`value="already-there"`,
		"已在目录",
		`action="/admin/ai/providers/models/append"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("候选弹窗缺少 %q", want)
		}
	}
	if strings.Contains(body, `{{`) {
		t.Fatal("渲染结果里残留未解析的模板标记")
	}
}

// TestAIModelsAppendKeepsExistingRows 勾选追加只增不删：已有行原样保留、重复提交不产生重复行。
//
// 「静默出错」的形态有两个：整组覆盖（把用户手填的行删掉）与重复追加（同一 id 两行）——
// 两者都不会报错，只会让目录悄悄变形，所以这里逐个钉住。
func TestAIModelsAppendKeepsExistingRows(t *testing.T) {
	ph, svc, _ := newAIPageEnv(t)
	ctx := t.Context()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "acme-gateway", DisplayName: "Acme Gateway"})
	if err != nil {
		t.Fatalf("新建供应商失败：%v", err)
	}
	if _, err = svc.SaveModels(ctx, &aidto.SaveModelsReq{
		ProviderID: p.ID, Version: p.Version,
		Models: []aidto.ModelEntry{{ID: "existing", DisplayName: "已有行"}},
	}); err != nil {
		t.Fatalf("准备目录失败：%v", err)
	}

	rec := serveAIPage(t, ph, http.MethodPost, "/admin/ai/providers/models/append",
		"providerId="+strconv.FormatInt(p.ID, 10)+"&modelIds=existing&modelIds=new-a&modelIds=new-a&modelIds=")
	assertAIJump(t, rec, true, "已添加 1 个模型。")

	after, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	ids := make([]string, 0, len(after.Models))
	for i := range after.Models {
		ids = append(ids, after.Models[i].ID)
		if after.Models[i].ID == "existing" && after.Models[i].DisplayName != "已有行" {
			t.Errorf("已有行被覆盖：%+v", after.Models[i])
		}
	}
	if len(ids) != 2 || ids[0] != "existing" || ids[1] != "new-a" {
		t.Fatalf("追加结果异常（应只增不删、不重复）：%v", ids)
	}
}

// TestAIModelsAppendRejectsEmptySelection 一个都没勾（或勾中的都已在目录）时报错，而不是「成功」。
func TestAIModelsAppendRejectsEmptySelection(t *testing.T) {
	ph, svc, _ := newAIPageEnv(t)
	ctx := t.Context()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "acme-gateway", DisplayName: "Acme Gateway"})
	if err != nil {
		t.Fatalf("新建供应商失败：%v", err)
	}
	rec := serveAIPage(t, ph, http.MethodPost, "/admin/ai/providers/models/append",
		"providerId="+strconv.FormatInt(p.ID, 10)+"&modelIds=&modelIds=++")
	assertAIJump(t, rec, false, "请先勾选要添加的模型。")
}

// TestAISessionsPageRenders 会话页渲染完整（空列表态）：双标签 + 看板 + 六项筛选 + 8 列明细。
//
// 断言清单是按「模板新增了什么」逐条钉的：标签 id、指标卡容器、新筛选控件、列数。
// 只断言「含 AI 会话」这种词的话，把整个看板删掉也照样绿。
func TestAISessionsPageRenders(t *testing.T) {
	body := renderAITemplate(t, "admin/ai/sessions", sessionPageData())

	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	for _, want := range []string{
		`action="/admin/ai/sessions"`,
		`name="keyword"`,
		"AI 会话",
		// 双标签：会话在前、模型在后，两块面板都在（服务端一次渲染完）。
		`id="ai-tab-sessions"`,
		`id="ai-tab-models"`,
		`id="ai-panel-sessions"`,
		`id="ai-panel-models"`,
		// 指标卡与趋势卡的容器。
		`class="stat-grid"`,
		// 六项筛选：关键词 + 状态 + 供应商 + 模型 + 创建人 + 起止日期。
		`name="status"`,
		`name="provider"`,
		`name="model"`,
		`name="user"`,
		`name="from"`,
		`name="to"`,
		// 明细 8 列，空态跨列数必须与表头一致。
		`colspan="7"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("会话页缺少 %q", want)
		}
	}
	// 没有数据时不该出现趋势图（.chart-trend 有定高，空着会留一条 160px 的空白带）。
	if strings.Contains(body, `class="chart-trend"`) {
		t.Fatal("趋势为空时不该渲染图表容器")
	}
	if !strings.Contains(body, "这段时间还没有事件") {
		t.Fatal("趋势为空时应给一句说明")
	}
}

// TestAISessionsPageRendersTrendAndUsage 看板拿到数据时渲染柱高与指标值。
//
// 柱高是服务端算好的百分比（模板不做算术）：这里钉的是「Height 原样进内联样式」
// 与「Height 为 0 的柱子带 is-empty」，以及指标卡直接用 Service 给好的格式化文本。
func TestAISessionsPageRendersTrendAndUsage(t *testing.T) {
	data := sessionPageData()
	data["Usage"] = aidto.SessionUsage{
		Sessions: 3, Events: 9, Tokens: 120, Compacts: 1, AvgTokens: 40,
		TokensText: "120", AvgTokensText: "40",
	}
	data["Trend"] = aidto.SessionTrend{
		From: "2026-10-01", To: "2026-10-03", Peak: 120, PeakText: "120",
		Series: []aidto.TrendSeries{
			{ProviderKey: "opencode-go", ModelID: "muse-spark", Total: 100, TotalText: "100",
				Color: 1, Points: "0,200 500,12 1000,120"},
			// 529 之前的历史事件：没有来源可记，图例里单独成一条，不能并进上面那家。
			{Total: 20, TotalText: "20", Color: 2, Points: "0,200 500,180 1000,190"},
			// 超出上限被合并的那条：明说含几家，否则用户会以为漏了。
			{OtherCount: 3, Total: 5, TotalText: "5", Color: 8, Points: "0,200 500,199 1000,199"},
		},
	}
	body := renderAITemplate(t, "admin/ai/sessions", data)

	for _, want := range []string{
		`class="chart-trend"`,
		`class="chart-trend-svg"`,
		`points="0,200 500,12 1000,120"`,
		`stroke: var(--chart-c1)`,
		`stroke: var(--chart-c8)`,
		"opencode-go / muse-spark",
		"未记录",
		"其他（3）",
		"2026-10-01 – 2026-10-03",
		"120",
		"40",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("趋势/指标卡缺少 %q", want)
		}
	}
}

// TestAISessionsPageModelsTabSelected 带 tab=models 时第二个标签默认展开、第一个隐藏。
//
// 两个标签是同一份 HTML 里的两块面板，选中态只体现在 aria-selected / hidden 上 ——
// 这两处任一处写错，页面上看到的都是「点进去还是第一页」。
func TestAISessionsPageModelsTabSelected(t *testing.T) {
	data := sessionPageData()
	data["IsModels"] = true
	data["Tab"] = "models"
	body := renderAITemplate(t, "admin/ai/sessions", data)

	if !strings.Contains(body, `id="ai-tab-models" aria-controls="ai-panel-models" aria-selected="true"`) {
		t.Fatal("模型标签应为选中态")
	}
	if !strings.Contains(body, `id="ai-tab-sessions" aria-controls="ai-panel-sessions" aria-selected="false"`) {
		t.Fatal("会话标签应为未选中态")
	}
	if strings.Contains(body, `id="ai-panel-models" aria-labelledby="ai-tab-models" hidden`) {
		t.Fatal("模型面板不该被隐藏")
	}
	if !strings.Contains(body, `id="ai-panel-sessions" aria-labelledby="ai-tab-sessions" hidden`) {
		t.Fatal("会话面板应被隐藏")
	}
	// 两个标签都在同一份 HTML 里（切标签不重新请求）。
	if !strings.Contains(body, `id="ai-panel-sessions"`) || !strings.Contains(body, `id="ai-panel-models"`) {
		t.Fatal("两块面板都必须渲染出来")
	}
}

// TestAIProvidersSaveValidationRedirects 校验失败：失败提示页，回跳指向统一入口。
func TestAIProvidersSaveValidationRedirects(t *testing.T) {
	ph, _, _ := newAIPageEnv(t)
	rec := serveAIPage(t, ph, http.MethodPost, "/admin/ai/providers/save", "providerKey=&displayName=")
	assertAIJump(t, rec, false, "请填写显示名称")
	if !strings.Contains(rec.Body.String(), `href="/admin/ai/sessions"`) {
		t.Fatal("回跳应指向大模型管理页")
	}
}

// TestAIProvidersSaveSuccessRedirectsAndPersists 成功：成功提示页 + 真落库。
func TestAIProvidersSaveSuccessRedirectsAndPersists(t *testing.T) {
	ph, svc, _ := newAIPageEnv(t)
	rec := serveAIPage(t, ph, http.MethodPost, "/admin/ai/providers/save",
		"providerKey=deepseek&displayName=深度求索&apiKey=sk-ai-test-plain-0123456789abcdef")
	assertAIJump(t, rec, true, "已保存")
	rows, err := svc.ListProviders(t.Context())
	if err != nil {
		t.Fatalf("回读供应商失败：%v", err)
	}
	if len(rows) != 1 || rows[0].ProviderKey != "deepseek" || !rows[0].HasAPIKey {
		t.Fatalf("成功提交应落库一条带密钥的 deepseek，实际 %+v", rows)
	}
}
