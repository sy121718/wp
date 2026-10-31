package feature

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

func newAIPageEnv(t *testing.T) (*aihttp.PageHandle, *aiservice.Service, *gorm.DB) {
	t.Helper()
	svc, db := newAIProviderService(t)
	return aihttp.NewPageHandle(svc), svc, db
}

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

func renderAITemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := templates.NewJetHTMLRender("../../../../internal/templates", true).Instance(name, data).Render(rec); err != nil {
		t.Fatalf("模板 %s 渲染失败：%v", name, err)
	}
	return rec.Body.String()
}

func pageBaseData(title string) gin.H {
	return gin.H{
		"title":      title,
		"csrf_token": "test-token",
		"t":          func(key, fallback string) string { return fallback },

		"Buttons": map[string]bool{
			"ai.provider_list": true, "ai.provider_get": true, "ai.provider_models_list": true,
			"ai.provider_save": true, "ai.provider_delete": true, "ai.provider_status": true,
			"ai.provider_models_save": true, "ai.provider_models_restore": true, "ai.provider_models_fetch": true,
			"ai.session_list": true, "ai.session_get": true, "ai.session_events": true,
			"ai.session_fold_plan": true, "ai.session_append": true, "ai.session_rename": true,
			"ai.session_archive": true, "ai.session_fold": true,

			"ai.chat": true,
		},
		"Err":  "",
		"Done": "",
	}
}

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

func TestAISessionsPageRendersUsageHovercard(t *testing.T) {
	data := sessionPageData()
	data["Rows"] = []gin.H{{
		"Session": aidto.Session{
			ID: 3, SessionKey: "k-3", Title: "会话三", Status: 1,
			EventCount: 4, ContextTokens: 15,
		},
		"Usage": []aidto.SessionModelUsage{
			{ProviderKey: "opencode-go", ModelID: "muse-spark", Events: 2, Tokens: 12, TokensText: "12"},

			{Unrecorded: true, Events: 2, Tokens: 3, TokensText: "3"},
		},

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

		"第三方模型提供商",
		"自定义模型 API",
		"OpenAI",

		"id=\"ai-picker-host\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("模型标签缺少 %q", want)
		}
	}

	mi := strings.Index(body, "id=\"ai-tab-models\"")
	si := strings.Index(body, "id=\"ai-tab-sessions\"")
	if mi < 0 || si < 0 || mi > si {
		t.Fatalf("标签顺序应为模型在前，实际 models=%d sessions=%d", mi, si)
	}
	if strings.Contains(body, `{{`) {
		t.Fatal("渲染结果里残留未解析的模板标记")
	}
}

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

func TestAISessionsPageRenders(t *testing.T) {
	body := renderAITemplate(t, "admin/ai/sessions", sessionPageData())

	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	for _, want := range []string{
		`action="/admin/ai/sessions"`,
		`name="keyword"`,
		"AI 会话",

		`id="ai-tab-sessions"`,
		`id="ai-tab-models"`,
		`id="ai-panel-sessions"`,
		`id="ai-panel-models"`,

		`class="stat-grid"`,

		`name="status"`,
		`name="provider"`,
		`name="model"`,
		`name="user"`,
		`name="from"`,
		`name="to"`,

		`colspan="7"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("会话页缺少 %q", want)
		}
	}

	if strings.Contains(body, `class="chart-trend"`) {
		t.Fatal("趋势为空时不该渲染图表容器")
	}
	if !strings.Contains(body, "这段时间还没有事件") {
		t.Fatal("趋势为空时应给一句说明")
	}
}

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

			{Total: 20, TotalText: "20", Color: 2, Points: "0,200 500,180 1000,190"},

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

	if !strings.Contains(body, `id="ai-panel-sessions"`) || !strings.Contains(body, `id="ai-panel-models"`) {
		t.Fatal("两块面板都必须渲染出来")
	}
}

func TestAIProvidersSaveValidationRedirects(t *testing.T) {
	ph, _, _ := newAIPageEnv(t)
	rec := serveAIPage(t, ph, http.MethodPost, "/admin/ai/providers/save", "providerKey=&displayName=")
	assertAIJump(t, rec, false, "请填写显示名称")
	if !strings.Contains(rec.Body.String(), `href="/admin/ai/sessions"`) {
		t.Fatal("回跳应指向大模型管理页")
	}
}

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
