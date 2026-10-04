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
//  2. 成功提交走 PRG（303）且真的落库；
//  3. 校验失败走 PRG（303）回列表页，提示以 i18n key 形式挂在 query 上。

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
		"PermSet": map[string]bool{
			"ai:provider_list": true, "ai:provider_get": true, "ai:provider_models_list": true,
			"ai:provider_save": true, "ai:provider_delete": true, "ai:provider_status": true,
			"ai:provider_models_save": true, "ai:provider_models_restore": true, "ai:provider_models_fetch": true,
			"ai:session_list": true, "ai:session_get": true, "ai:session_events": true,
			"ai:session_fold_plan": true, "ai:session_append": true, "ai:session_rename": true,
			"ai:session_archive": true, "ai:session_fold": true,
		},
		"Err":  "",
		"Done": "",
	}
}

// TestAIProvidersPageRenders 供应商页渲染完整、且有可用的新建入口。
func TestAIProvidersPageRenders(t *testing.T) {
	data := pageBaseData("模型")
	data["Providers"] = []aidto.Provider{}
	data["BuiltinKeys"] = aiservice.BuiltinProviderKeys()
	data["ProtocolOptions"] = aienums.ProtocolOptions
	data["Presets"] = aiservice.BuiltinPresets()
	body := renderAITemplate(t, "admin/ai/providers", data)

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
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("供应商页缺少 %q", want)
		}
	}
	if strings.Contains(body, `{{`) {
		t.Fatal("渲染结果里残留未解析的模板标记")
	}
}

// TestAIProvidersPageRendersModelPicker 候选弹窗整块渲染（含「已在目录」的 disable 分支）。
//
// 这条路径上最容易写错的是模板里对 map 的取值：`{{existing[id]}}` 与 `isset(...)`
// 只有真渲一次才知道语法对不对 —— 错了就是运行时整页 500。
func TestAIProvidersPageRendersModelPicker(t *testing.T) {
	data := pageBaseData("模型")
	data["Providers"] = []aidto.Provider{}
	data["BuiltinKeys"] = aiservice.BuiltinProviderKeys()
	data["ProtocolOptions"] = aienums.ProtocolOptions
	data["Presets"] = aiservice.BuiltinPresets()
	data["Picker"] = gin.H{
		"Provider":   aidto.Provider{ID: 7, ProviderKey: "acme-gateway", DisplayName: "Acme Gateway", Version: 2},
		"Candidates": []string{"gpt-4o", "already-there"},
		"Existing":   map[string]bool{"already-there": true},
	}
	body := renderAITemplate(t, "admin/ai/providers", data)

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
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("追加应走 PRG（303），实际 %d（body=%s）", rec.Code, rec.Body.String())
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
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("空选择应走 PRG（303），实际 %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "err="+aienums.ErrNoModelSelected) {
		t.Fatalf("空选择应回 %s，实际 Location=%s", aienums.ErrNoModelSelected, rec.Header().Get("Location"))
	}
}

// TestAISessionsPageRenders 会话页渲染完整（空列表态）。
func TestAISessionsPageRenders(t *testing.T) {
	data := pageBaseData("AI 会话")
	data["Rows"] = []aidto.Session{}
	data["Total"] = 0
	data["Page"] = 1
	data["Keyword"] = ""
	data["Status"] = -1
	body := renderAITemplate(t, "admin/ai/sessions", data)

	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	for _, want := range []string{
		`action="/admin/ai/sessions"`,
		`name="keyword"`,
		"AI 会话",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("会话页缺少 %q", want)
		}
	}
}

// TestAIProvidersSaveValidationRedirects 校验失败：303 回列表页，提示挂在 ?err= 上。
func TestAIProvidersSaveValidationRedirects(t *testing.T) {
	ph, _, _ := newAIPageEnv(t)
	rec := serveAIPage(t, ph, http.MethodPost, "/admin/ai/providers/save", "providerKey=&displayName=")

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("校验失败应 303 回列表页，实际 %d（%s）", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/ai/providers?") {
		t.Fatalf("回跳地址应指向供应商页，实际 %q", loc)
	}
	if !strings.Contains(loc, "err=") {
		t.Fatalf("回跳地址应带 err 提示，实际 %q", loc)
	}
	if strings.HasSuffix(loc, "err=") {
		t.Fatal("err 提示为空：错误出口没有落地")
	}
}

// TestAIProvidersSaveSuccessRedirectsAndPersists 成功：303 + done 提示 + 真落库。
func TestAIProvidersSaveSuccessRedirectsAndPersists(t *testing.T) {
	ph, svc, _ := newAIPageEnv(t)
	rec := serveAIPage(t, ph, http.MethodPost, "/admin/ai/providers/save",
		"providerKey=deepseek&displayName=深度求索&apiKey=sk-ai-test-plain-0123456789abcdef")

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("成功提交应 303 回列表页，实际 %d（%s）", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "done=") {
		t.Fatalf("成功回跳应带 done 提示，实际 %q", loc)
	}
	rows, err := svc.ListProviders(t.Context())
	if err != nil {
		t.Fatalf("回读供应商失败：%v", err)
	}
	if len(rows) != 1 || rows[0].ProviderKey != "deepseek" || !rows[0].HasAPIKey {
		t.Fatalf("成功提交应落库一条带密钥的 deepseek，实际 %+v", rows)
	}
}
