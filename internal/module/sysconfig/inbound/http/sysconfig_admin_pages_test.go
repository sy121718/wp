package sysconfighttp_test

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfighttp "go_wp/internal/module/sysconfig/inbound/http"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func newSystemPageEnv(t *testing.T) (*sysconfighttp.AdminHandle, *sysconfigservice.Service, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Skip("本地 PostgreSQL 不可用，跳过")
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行种子数据失败: %v", err)
	}
	svc := sysconfigservice.NewService(sysconfigmodel.NewSysConfigModel(db), nil)
	return sysconfighttp.NewAdminHandle(svc), svc, db
}

func serve(t *testing.T, handle *sysconfighttp.AdminHandle, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.GET("/admin/system", handle.SystemPage)
	engine.POST("/api/sysconfig/system/save", pageview.HTMLFragment, handle.APISystemSave)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestSystemSettingsPageRenders(t *testing.T) {
	_, svc, db := newSystemPageEnv(t)
	ctx := context.Background()
	i18nGroup, err := svc.GetGroup(ctx, "i18n")
	if err != nil {
		t.Fatalf("读 i18n 组失败: %v", err)
	}
	tradeGroup, err := svc.GetGroup(ctx, "trade")
	if err != nil {
		t.Fatalf("读 trade 组失败: %v", err)
	}
	langs, lerr := svc.ListDictOptions(ctx, "language")
	currencies, cerr := svc.ListDictOptions(ctx, "currency")
	countries, coerr := svc.ListCountryOptions(ctx, "zh-CN")
	if lerr != nil || cerr != nil || coerr != nil {
		t.Fatalf("读字典失败: %v / %v / %v", lerr, cerr, coerr)
	}
	data := gin.H{
		"title": "系统设置", "csrf_token": "test-token",
		"t":               func(key, fallback string) string { return fallback },
		"I18N":            i18nGroup,
		"Trade":           tradeGroup,
		"LangOptions":     langs,
		"CurrencyOptions": currencies,
		"CountryOptions":  countries,
		"ModeOptions":     []string{"default_plain", "all_prefix", "off"},
		"Err":             "",
		"Done":            "",
	}
	rec := httptest.NewRecorder()
	if err := templates.NewJetHTMLRender("../../../../templates", true).
		Instance("admin/system/settings", data).Render(rec); err != nil {
		t.Fatalf("系统设置页渲染失败: %v", err)
	}
	body := rec.Body.String()
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("整页没有渲染完（缺 </html>）：模板在某一行中断了")
	}
	for _, want := range []string{
		`name="defaultLang"`, `name="siteLangURLMode"`,
		`name="defaultCountry"`, `name="defaultCurrency"`,
		`name="i18nVersion"`, `name="tradeVersion"`,
		`hx-post="/api/sysconfig/system/save"`,
		`value="zh-CN"`,
		`value="CNY"`,
		`value="CN"`,
		"系统设置",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("系统设置页缺少 %q", want)
		}
	}

	if !strings.Contains(body, `class="form-grid"`) {
		t.Fatalf("四个字段应放在 .form-grid 里（响应式两列/单列）")
	}
	if strings.Contains(body, "px;width:") || strings.Contains(body, `style="width:`) {
		t.Fatalf("页面不该出现内联固定宽度（会撑破窄屏）")
	}

	var disabled string
	if err := db.Raw("SELECT code FROM sys_dict WHERE type='language' AND enabled = false LIMIT 1").Scan(&disabled).Error; err == nil && disabled != "" {
		if strings.Contains(body, `value="`+disabled+`"`) {
			t.Fatalf("停用的字典项 %q 出现在下拉里", disabled)
		}
	}
	t.Logf("渲染 %d 字节；语言选项 %d 项、货币 %d 项、国家 %d 项（含停用项已排除）",
		len(body), len(langs), len(currencies), len(countries))
}

func TestSystemSettingsSavePreservesUnexposedKeys(t *testing.T) {
	handle, svc, _ := newSystemPageEnv(t)
	ctx := context.Background()

	group, err := svc.GetGroup(ctx, "i18n")
	if err != nil {
		t.Fatalf("读 i18n 组失败: %v", err)
	}
	before, _ := group.Data["lang_url_codes"].(map[string]any)
	if len(before) == 0 {
		t.Skip("本库 i18n 组没有 lang_url_codes，跳过该陷阱的验证")
	}
	t.Logf("保存前 lang_url_codes = %v；default_lang = %v", before, group.Data["default_lang"])

	form := url.Values{"i18nVersion": {fmt.Sprint(group.Version)}, "tradeVersion": {fmt.Sprint(tradeVersion(t, svc))},
		"defaultLang": {"en-US"}, "siteLangURLMode": {"default_plain"}, "defaultCountry": {"CN"}, "defaultCurrency": {"CNY"}}
	rec := serve(t, handle, http.MethodPost, "/api/sysconfig/system/save", form.Encode())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("保存回执应为 HTML 200: %d %s", rec.Code, rec.Body.String())
	}
	after, err := svc.GetGroup(ctx, "i18n")
	if err != nil {
		t.Fatalf("保存后读 i18n 组失败: %v", err)
	}
	got, _ := after.Data["lang_url_codes"].(map[string]any)
	if len(got) != len(before) {
		t.Fatalf("保存把未暴露的键冲掉了：前 %v，后 %v", before, got)
	}
	if after.Data["default_lang"] != "en-US" {
		t.Fatalf("default_lang 应已改为 en-US，实际 %v", after.Data["default_lang"])
	}
	t.Logf("保存后 lang_url_codes = %v（保留），default_lang = %v（已改）", got, after.Data["default_lang"])
}

func TestSystemSettingsSaveRejectsStaleVersion(t *testing.T) {
	handle, svc, _ := newSystemPageEnv(t)
	ctx := context.Background()
	group, err := svc.GetGroup(ctx, "i18n")
	if err != nil {
		t.Fatalf("读 i18n 组失败: %v", err)
	}
	form := url.Values{"i18nVersion": {fmt.Sprint(group.Version)}, "tradeVersion": {fmt.Sprint(tradeVersion(t, svc))},
		"defaultLang": {"en-US"}, "siteLangURLMode": {"default_plain"}, "defaultCountry": {"CN"}, "defaultCurrency": {"CNY"}}
	if _, err := svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{GroupKey: "i18n", Version: group.Version, Data: group.Data}); err != nil {
		t.Fatal(err)
	}
	rec := serve(t, handle, http.MethodPost, "/api/sysconfig/system/save", form.Encode())
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("旧版本应返回 HTML 409: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := svc.GetGroup(ctx, "i18n")
	if after.Version != group.Version+1 {
		t.Fatalf("冲突提交不该写库：版本从 %d 变成了 %d", group.Version, after.Version)
	}
	t.Logf("旧版本提交被拒：提示页失败态，版本仍为 %d", after.Version)
}

func TestSystemSettingsTradeConflictRollsBackI18N(t *testing.T) {
	handle, svc, _ := newSystemPageEnv(t)
	ctx := context.Background()
	i18nBefore, err := svc.GetGroup(ctx, "i18n")
	if err != nil {
		t.Fatal(err)
	}
	tradeBefore, err := svc.GetGroup(ctx, "trade")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetGroup(ctx, &sysconfigdto.SetGroupReq{GroupKey: "trade", Version: tradeBefore.Version, Data: tradeBefore.Data}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"i18nVersion": {fmt.Sprint(i18nBefore.Version)}, "tradeVersion": {fmt.Sprint(tradeBefore.Version)},
		"defaultLang": {"en-US"}, "siteLangURLMode": {"default_plain"}, "defaultCountry": {"CN"}, "defaultCurrency": {"CNY"}}
	rec := serve(t, handle, http.MethodPost, "/api/sysconfig/system/save", form.Encode())
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale trade version: %d %s", rec.Code, rec.Body.String())
	}
	i18nAfter, err := svc.GetGroup(ctx, "i18n")
	if err != nil {
		t.Fatal(err)
	}
	tradeAfter, err := svc.GetGroup(ctx, "trade")
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, err := json.Marshal(i18nBefore.Data)
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, err := json.Marshal(i18nAfter.Data)
	if err != nil {
		t.Fatal(err)
	}
	if i18nAfter.Version != i18nBefore.Version || string(afterJSON) != string(beforeJSON) {
		t.Fatalf("trade conflict changed i18n: before=%+v after=%+v", i18nBefore, i18nAfter)
	}
	if tradeAfter.Version != tradeBefore.Version+1 {
		t.Fatalf("trade changed during failed aggregate: before=%d after=%d", tradeBefore.Version, tradeAfter.Version)
	}
}

func tradeVersion(t *testing.T, svc *sysconfigservice.Service) int64 {
	t.Helper()
	g, err := svc.GetGroup(context.Background(), "trade")
	if err != nil {
		t.Fatalf("读 trade 组失败: %v", err)
	}
	return g.Version
}
