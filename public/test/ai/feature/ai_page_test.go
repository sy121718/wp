package feature

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aihttp "go_wp/internal/module/ai/inbound/http"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// newAIPageEnv 交一份「供应商 + 模型目录」的写入口。
//
// 页面侧的 PageHandle 已经退役：这些写操作只有 /api 一条入口，所以这里直接给 API
// handler（与 internal/module/ai/inbound/http/ai_router.go 里注册的是同一个）。
func newAIPageEnv(t *testing.T) (*aihttp.Handle, *aiservice.Service, *gorm.DB) {
	t.Helper()
	svc, db := newAIProviderService(t)
	return aihttp.NewHandle(svc), svc, db
}

// aiJSON 是 ai 页写动作的响应信封：非页面请求一律 {code, message, data} + 真实状态码。
type aiJSON struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

func decodeAIJSON(t *testing.T, rec *httptest.ResponseRecorder) aiJSON {
	t.Helper()
	var out aiJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON 信封：%v，body=%s", err, rec.Body.String())
	}
	return out
}

// assertAIOK 断言一次写动作成功：200 + JSON + code 0 + 文案精确等于 want。
//
// want 为空时只校验「有文案」。非空时按整句比对 —— 成功文案也归词库，
// 用 Contains 会放过「文案被拼进了别的东西」这类回归。
func assertAIOK(t *testing.T, rec *httptest.ResponseRecorder, want string) aiJSON {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("成功应为 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.Contains(ctype, "application/json") {
		t.Fatalf("非页面请求应回 JSON，实际 Content-Type=%q", ctype)
	}
	if body := rec.Body.String(); strings.Contains(body, "<html") {
		t.Fatalf("非页面请求不该回 HTML：%s", body)
	}
	out := decodeAIJSON(t, rec)
	if out.Code != http.StatusOK {
		t.Fatalf("成功码应为 0，实际 %d（message=%s）", out.Code, out.Message)
	}
	if want == "" {
		if out.Message == "" {
			t.Fatal("成功应有回执文案")
		}
		return out
	}
	if out.Message != want {
		t.Fatalf("文案应为 %q，实际 %q", want, out.Message)
	}
	return out
}

// assertAIFail 断言一次写动作失败：真实状态码 + JSON + code 与状态同值 + 文案精确。
//
// 失败信封的 code 就是 HTTP 状态码（pkg/response 的 ErrorWithMessage 把同一个值写进
// 头与 code），所以这里断言两者相等，而不是像成功那样 code==0 —— 成功是 200 + code 0，
// 失败是 4xx/5xx + code 同值，混成一条会让「服务端把失败当成功返回」永远测不出来。
//
// want 非空时按**整句精确**比对：文案归词库，测试要锁住的就是那一句；退回 Contains
// 会放过「文案被拼进了别的东西」这类回归。
func assertAIFail(t *testing.T, rec *httptest.ResponseRecorder, status int, want string) aiJSON {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("应为 %d，实际 %d：%s", status, rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.Contains(ctype, "application/json") {
		t.Fatalf("失败也应回 JSON，实际 Content-Type=%q", ctype)
	}
	if body := rec.Body.String(); strings.Contains(body, "<html") {
		t.Fatalf("失败不该回 HTML：%s", body)
	}
	out := decodeAIJSON(t, rec)
	if out.Code != status {
		t.Fatalf("失败信封的 code 应与状态码同值：code=%d status=%d", out.Code, status)
	}
	if out.Message == "" {
		t.Fatal("失败必须有 message")
	}
	// 声明 key 与内部原文一律不上屏。
	for _, leak := range []string{"ai.err.", "ai.msg.", "Err", "SQLSTATE", "duplicate key", "constraint"} {
		if strings.Contains(out.Message, leak) {
			t.Fatalf("message 漏出内部标识 %q：%q", leak, out.Message)
		}
	}
	if want != "" && out.Message != want {
		t.Fatalf("文案应为 %q，实际 %q", want, out.Message)
	}
	return out
}

func serveAIPage(t *testing.T, h *aihttp.Handle, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们片段渲染为空。
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	// 与 ai_router.go 注册的路径逐字一致：页面侧不再有 /admin 转发路由。
	engine.POST("/api/ai/provider/save", pageview.HTMLFragment, h.SaveProvider)
	engine.POST("/api/ai/provider/delete", pageview.HTMLFragment, h.DeleteProvider)
	engine.POST("/api/ai/provider/status", pageview.HTMLFragment, h.SetProviderStatus)
	engine.POST("/api/ai/provider/models/candidates", pageview.HTMLFragment, h.ModelCandidates)
	engine.POST("/api/ai/provider/models/append", pageview.HTMLFragment, h.AppendModels)
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
			"ai.provider_models_save": true, "ai.provider_models_restore": true,
			// 候选与追加是独立权限点：候选按钮打 candidates（会解密密钥打远端 /models），
			// 追加表单打 append（会改写目录）。夹具要按真实权限码给，否则弹窗整块不渲染。
			"ai.provider_models_candidates": true, "ai.provider_models_append": true,
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
		// 夹具按 DTO 的事实形状给：token 是整数、耗时是毫秒、错误只给 key。
		// 显示文本（1.2M / 2.90 s / 归口译文）由模板函数与全局 t 现算。
		"Usage": []aidto.SessionModelUsage{
			{ProviderKey: "opencode-go", ModelID: "muse-spark", Events: 2, Tokens: 12},

			{Unrecorded: true, Events: 2, Tokens: 3},
		},

		"Calls": aidto.SessionCalls{
			Total: 7,
			Rows: []aidto.SessionCallRow{
				{At: utils.NewJSONTime(time.Date(2026, 10, 4, 14, 12, 0, 0, time.Local)), ProviderKey: "opencode-go", ModelID: "muse-spark", LatencyMs: 2900, Tokens: 465, OK: true},
				{At: utils.NewJSONTime(time.Date(2026, 10, 4, 13, 40, 0, 0, time.Local)), ProviderKey: "opencode-go", ModelID: "muse-spark", LatencyMs: 480, Tokens: 0, ErrorKey: aienums.ErrInternal},
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
		// 耗时按 latency 函数显示：≥1000ms 走秒（两位小数），<1000ms 走毫秒。
		"2.90 s",
		"480 ms",
		"成功",
		// 失败格按 key 取词（缺词落「调用失败」兜底）：这里断言它没有裸 key。
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
		`hx-post="/api/ai/provider/save"`,
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
	data["Provider"] = aidto.Provider{ID: 7, ProviderKey: "acme-gateway", DisplayName: "Acme Gateway", Version: 2}
	data["Candidates"] = []string{"gpt-4o", "already-there"}
	data["Existing"] = map[string]bool{"already-there": true}
	body := renderAITemplate(t, "admin/ai/provider_picker", data)
	if strings.Contains(body, "<html") || strings.Contains(body, "<!doctype") {
		t.Fatal("候选弹窗应是片段，不该带整页壳")
	}

	for _, want := range []string{
		"选择要添加的模型",
		"以下是模型提供商的可用模型",
		"搜索模型",
		"添加所选",
		`name="modelIds" value="gpt-4o"`,
		`name="modelIds" value="already-there"`,
		`data-check-item disabled`,
		"已在目录",
		`hx-post="/api/ai/provider/models/append"`,
		`data-modal-auto-open`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("候选弹窗缺少 %q", want)
		}
	}
	if strings.Contains(body, `{{`) {
		t.Fatal("渲染结果里残留未解析的模板标记")
	}
}

// TestAIModelsAppendRejectsStaleVersion 钉住乐观锁：带上过期的版本号追加必须被 409 打回，
// 且目录一个字节都没变。缺版本号是**另一类**错误（400 参数不完整），也别混成一类。
func TestAIModelsAppendRejectsStaleVersion(t *testing.T) {
	h, svc, _ := newAIPageEnv(t)
	ctx := t.Context()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "stale-gateway", DisplayName: "Stale Gateway"})
	if err != nil {
		t.Fatalf("新建供应商失败：%v", err)
	}
	if _, err = svc.SaveModels(ctx, &aidto.SaveModelsReq{
		ProviderID: p.ID, Version: p.Version,
		Models: []aidto.ModelEntry{{ID: "keep"}},
	}); err != nil {
		t.Fatalf("准备目录失败：%v", err)
	}
	cur, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读版本号失败：%v", err)
	}

	// ① 缺版本号：参数不完整，不是「版本不对」。
	rec := serveAIPage(t, h, http.MethodPost, "/api/ai/provider/models/append",
		"providerId="+strconv.FormatInt(p.ID, 10)+"&modelIds=new-a")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺版本号应为 400，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// ② 过期版本：被别人抢先写过，必须整批拒绝。
	rec = serveAIPage(t, h, http.MethodPost, "/api/ai/provider/models/append",
		"providerId="+strconv.FormatInt(p.ID, 10)+
			"&version="+strconv.FormatInt(cur.Version-1, 10)+
			"&modelIds=new-a")
	if rec.Code != http.StatusConflict {
		t.Fatalf("陈旧版本应为 409，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 目录必须还是原样：乐观锁的意义就是「拒绝时不留半个改动」。
	after, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if len(after.Models) != 1 || after.Models[0].ID != "keep" {
		t.Fatalf("被拒绝的追加不该改目录，实际 %+v", after.Models)
	}
}

func TestAIModelsAppendKeepsExistingRows(t *testing.T) {
	h, svc, _ := newAIPageEnv(t)
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

	// 带真实版本号：追加是「读目录 → 合并 → 按版本号写回」，缺版本是参数错误（400），
	// 拿旧版本会被乐观锁打回（409）—— 两种情况都不能被静默放行。
	stored, err := svc.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("回读版本号失败：%v", err)
	}
	rec := serveAIPage(t, h, http.MethodPost, "/api/ai/provider/models/append",
		"providerId="+strconv.FormatInt(p.ID, 10)+
			"&version="+strconv.FormatInt(stored.Version, 10)+
			"&modelIds=existing&modelIds=new-a&modelIds=new-a&modelIds=")
	if rec.Code != http.StatusOK {
		t.Fatalf("追加应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("追加应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	if !strings.Contains(rec.Body.String(), "new-a") {
		t.Fatalf("追加片段应回新的模型目录：%s", rec.Body.String())
	}

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
	h, svc, _ := newAIPageEnv(t)
	ctx := t.Context()

	p, err := svc.SaveProvider(ctx, &aidto.SaveProviderReq{ProviderKey: "acme-gateway", DisplayName: "Acme Gateway"})
	if err != nil {
		t.Fatalf("新建供应商失败：%v", err)
	}
	rec := serveAIPage(t, h, http.MethodPost, "/api/ai/provider/models/append",
		"providerId="+strconv.FormatInt(p.ID, 10)+"&modelIds=&modelIds=++")
	// 文案与词库逐字一致（517_ai_preset_i18n.sql 的 ai.err.noModelSelected）。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空勾选应为 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) || !strings.Contains(body, "请先勾选要添加的模型。") {
		t.Fatalf("失败应渲染 Jet 错误片段且带译文：%s", body)
	}
	if strings.Contains(body, "ai.err.") {
		t.Fatalf("失败片段漏出了错误 key：%s", body)
	}
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
	}
	// 趋势只交事实：逐日 token 数组 + 总量。坐标与配色由 ai-trend.js 按 values 现算，
	// 所以这里没有 Points / Color / TotalText —— 那些是显示格式，不是服务端事实。
	data["Trend"] = aidto.SessionTrend{
		From: "2026-10-01", To: "2026-10-03", Peak: 120,
		Series: []aidto.TrendSeries{
			{ProviderKey: "opencode-go", ModelID: "muse-spark", Total: 100, Values: []int64{0, 108, 80}},

			{Total: 20, Values: []int64{0, 20, 10}},

			{OtherCount: 3, Total: 5, Values: []int64{0, 1, 1}},
		},
	}
	body := renderAITemplate(t, "admin/ai/sessions", data)

	// 趋势块只验「事实与骨架」：坐标由前端算，所以这里断言逐日数组与峰值都上了页面，
	// 而不再断言 points= / stroke=（那些由 ai-trend.js 生成，见 ai-trend.test.cjs）。
	for _, want := range []string{
		`class="chart-trend"`,
		`class="chart-trend-svg"`,
		`data-trend-svg`,
		`data-trend-peak="120"`,
		`data-trend-legend`,
		`data-trend-series="0"`,
		`data-values="0 108 80"`,
		`style="background: var(--chart-c1)"`,
		`style="background: var(--chart-c3)"`,
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

func TestAIProvidersSaveValidationIsHTMLFailure(t *testing.T) {
	h, _, _ := newAIPageEnv(t)
	rec := serveAIPage(t, h, http.MethodPost, "/api/ai/provider/save", "providerKey=&displayName=")
	// 校验失败是请求内容问题：400 + 词库文案，片段只进表单声明的错误区。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("校验失败应为 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) || !strings.Contains(body, "请填写显示名称") {
		t.Fatalf("失败应渲染 Jet 错误片段且带译文：%s", body)
	}
	if strings.Contains(body, "ai.err.") {
		t.Fatalf("失败片段漏出了错误 key：%s", body)
	}
}

func TestAIProvidersSaveSuccessIsHTMLAndPersists(t *testing.T) {
	h, svc, _ := newAIPageEnv(t)
	rec := serveAIPage(t, h, http.MethodPost, "/api/ai/provider/save",
		"providerKey=deepseek&displayName=深度求索&apiKey=sk-ai-test-plain-0123456789abcdef")
	if rec.Code != http.StatusOK {
		t.Fatalf("保存应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("保存应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	if !strings.Contains(rec.Body.String(), "已保存") {
		t.Fatalf("保存片段应带成功结论：%s", rec.Body.String())
	}
	rows, err := svc.ListProviders(t.Context())
	if err != nil {
		t.Fatalf("回读供应商失败：%v", err)
	}
	if len(rows) != 1 || rows[0].ProviderKey != "deepseek" || !rows[0].HasAPIKey {
		t.Fatalf("成功提交应落库一条带密钥的 deepseek，实际 %+v", rows)
	}
}
