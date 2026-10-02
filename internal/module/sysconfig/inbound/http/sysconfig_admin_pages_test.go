package sysconfighttp_test

// sysconfig_admin_pages_test.go — 系统设置页的**真实渲染 + 保存**链路（真库 + 真 Jet 引擎）。
//
// 这条链上最容易静默出错的三件事，各自钉在一条断言上：
//  1. 整组替换冲掉未暴露的键（i18n 组的 lang_url_codes）—— 保存后回读；
//  2. 乐观锁形同虚设（用旧 version 也能写进去）—— 用旧 version 提交必须被拒；
//  3. 下拉没有真的接到字典表（选项写死在模板里）—— 断言选项来自 sys_dict / sys_area。
//
// 走**直挂 handler** 而不是通过 SetupSysConfigRoutes：后者页面路由带 Casbin 中间件，
// 需要一套完整策略环境才跑得起来；本用例关心的是模板与数据（鉴权由 permission 包的既有测试覆盖）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	sysconfighttp "go_wp/internal/module/sysconfig/inbound/http"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	sysconfigservice "go_wp/internal/module/sysconfig/service"
	"go_wp/internal/templates"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// newSystemPageEnv 装配系统设置页的真实链路（真 PG + 真 Jet）。
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

// serve 把请求交给系统设置页 handler（真实 Jet 引擎）。
func serve(t *testing.T, handle *sysconfighttp.AdminHandle, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.GET("/admin/system", handle.SystemPage)
	engine.POST("/admin/system/save", handle.SystemSave)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestSystemSettingsPageRenders 真实渲染：四个字段 + 字典下拉 + version + 完整整页。
//
// 这里用**同一模板 + 与 handler 同形的数据**直渲，而不是经 HTTP 走 SetPage：
// 后台整页继承 layout.html，而 layout 的侧栏来自 shell.PermContextMiddleware 注入的
// 权限导航树（需要登录会话 + 权限服务）——测试环境不具备，走 HTTP 只会得到一个
// 「页面暂时无法显示」。数据源与 handler 一致（同样的 svc 调用与同样的键名），
// 因此模板与数据键对不上、下拉没接字典这类问题仍会被这条断言抓住。
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
		`action="/admin/system/save"`,
		`value="zh-CN"`, // 语言下拉来自 sys_dict（type=language）
		`value="CNY"`,   // 货币下拉来自 sys_dict（type=currency）
		`value="CN"`,    // 国家下拉来自 sys_area（kind=country）
		"系统设置",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("系统设置页缺少 %q", want)
		}
	}
	// 多端：四个字段排在 .form-grid 里（窄屏单列、宽屏两列，见 theme.css），
	// 且本页不写任何固定像素宽度 —— 容器宽度由外层 .stack/.card-body 决定。
	if !strings.Contains(body, `class="form-grid"`) {
		t.Fatalf("四个字段应放在 .form-grid 里（响应式两列/单列）")
	}
	if strings.Contains(body, "px;width:") || strings.Contains(body, `style="width:`) {
		t.Fatalf("页面不该出现内联固定宽度（会撑破窄屏）")
	}
	// 停用的字典项不该出现。
	var disabled string
	if err := db.Raw("SELECT code FROM sys_dict WHERE type='language' AND enabled = false LIMIT 1").Scan(&disabled).Error; err == nil && disabled != "" {
		if strings.Contains(body, `value="`+disabled+`"`) {
			t.Fatalf("停用的字典项 %q 出现在下拉里", disabled)
		}
	}
	t.Logf("渲染 %d 字节；语言选项 %d 项、货币 %d 项、国家 %d 项（含停用项已排除）",
		len(body), len(langs), len(currencies), len(countries))
}

// TestSystemSettingsSavePreservesUnexposedKeys 那个陷阱：整组替换不能冲掉 lang_url_codes。
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

	form := url.Values{
		"i18nVersion":     {strconv.FormatInt(group.Version, 10)},
		"tradeVersion":    {strconv.FormatInt(tradeVersion(t, svc), 10)},
		"defaultLang":     {"en-US"},
		"siteLangURLMode": {"default_plain"},
		"defaultCountry":  {"CN"},
		"defaultCurrency": {"CNY"},
	}
	rec := serve(t, handle, http.MethodPost, "/admin/system/save", form.Encode())
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("保存成功应 303 回本页，实际 %d（body=%s）", rec.Code, rec.Body.String())
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

// TestSystemSettingsSaveRejectsStaleVersion 乐观锁：旧 version 提交必须被拒且有可读提示。
func TestSystemSettingsSaveRejectsStaleVersion(t *testing.T) {
	handle, svc, _ := newSystemPageEnv(t)
	ctx := context.Background()
	group, err := svc.GetGroup(ctx, "i18n")
	if err != nil {
		t.Fatalf("读 i18n 组失败: %v", err)
	}
	form := url.Values{
		"i18nVersion":     {strconv.FormatInt(group.Version-1, 10)},
		"tradeVersion":    {strconv.FormatInt(tradeVersion(t, svc), 10)},
		"defaultLang":     {"en-US"},
		"siteLangURLMode": {"default_plain"},
		"defaultCountry":  {"CN"},
		"defaultCurrency": {"CNY"},
	}
	rec := serve(t, handle, http.MethodPost, "/admin/system/save", form.Encode())
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("冲突应 303 回本页并带 ?err=，实际 %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("冲突没有带回可读提示：Location=%s", loc)
	}
	after, _ := svc.GetGroup(ctx, "i18n")
	if after.Version != group.Version {
		t.Fatalf("冲突提交不该写库：版本从 %d 变成了 %d", group.Version, after.Version)
	}
	t.Logf("旧版本提交被拒：Location=%s（版本仍为 %d）", loc, after.Version)
}

func tradeVersion(t *testing.T, svc *sysconfigservice.Service) int64 {
	t.Helper()
	g, err := svc.GetGroup(context.Background(), "trade")
	if err != nil {
		t.Fatalf("读 trade 组失败: %v", err)
	}
	return g.Version
}
