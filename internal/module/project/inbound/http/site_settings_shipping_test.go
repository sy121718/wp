package projecthttp

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

type fakeShippingProjects struct {
	projectcontract.ProjectService
	settings json.RawMessage
	updated  json.RawMessage
	updates  int
}

func (f *fakeShippingProjects) Detail(_ context.Context, req *projectcontract.DetailReq) (*projectcontract.ProjectResp, error) {
	return &projectcontract.ProjectResp{ID: req.ID, Name: "站点", Settings: f.settings}, nil
}

func (f *fakeShippingProjects) Update(_ context.Context, req *projectcontract.UpdateReq) (*projectcontract.ProjectResp, error) {
	f.updates++
	f.updated = req.Settings
	return &projectcontract.ProjectResp{ID: req.ID, Name: req.Name, Settings: req.Settings}, nil
}

func postSiteSettingsSave(t *testing.T, projects projectcontract.ProjectService, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()

	router.HTMLRender = templates.NewJetHTMLRender(siteSettingsScriptsTemplateDir, true)
	h := NewSiteSettingsAdminHandle(projects, nil, nil)
	router.POST("/admin/settings/save", h.SaveSiteSettings)

	req := httptest.NewRequest(http.MethodPost, "/admin/settings/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func shippingSaveForm(extra map[string]string) url.Values {
	form := url.Values{"projectId": {"p1"}, "name": {"站点"}}
	for k, v := range extra {
		form.Set(k, v)
	}
	return form
}

func settingsOf(t *testing.T, f *fakeShippingProjects) map[string]json.RawMessage {
	t.Helper()
	if f.updates == 0 {
		t.Fatal("没有任何落库写入（保存被拒绝了？）")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(f.updated, &obj); err != nil {
		t.Fatalf("写入的 settings 不是 JSON 对象: %v", err)
	}
	return obj
}

func int64Of(t *testing.T, obj map[string]json.RawMessage, key string) int64 {
	t.Helper()
	raw, ok := obj[key]
	if !ok {
		return 0
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("键 %s 不是整数: %s", key, raw)
	}
	return v
}

func TestSaveSiteSettingsShippingFeeToCents(t *testing.T) {
	projects := &fakeShippingProjects{settings: json.RawMessage(`{}`)}
	rec := postSiteSettingsSave(t, projects, shippingSaveForm(map[string]string{
		"shippingBaseFee":       "8.00",
		"shippingFreeThreshold": "100",
	}))
	assertJumpOK(t, rec)
	obj := settingsOf(t, projects)
	if got := int64Of(t, obj, "shippingBaseFee"); got != 800 {
		t.Errorf("8.00 元应落库为 800 分，实际 %d", got)
	}
	if got := int64Of(t, obj, "shippingFreeThreshold"); got != 10000 {
		t.Errorf("100 元门槛应落库为 10000 分，实际 %d", got)
	}
}

func assertJumpOK(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("成功应 200 渲染提示页，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("响应不是成功提示页（缺 data-jump-state=\"ok\"）:\n%s", rec.Body.String())
	}
}

func assertJumpErr(t *testing.T, rec *httptest.ResponseRecorder, wantSub string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("失败应 200 渲染提示页，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("响应不是失败提示页（缺 data-jump-state=\"err\"）:\n%s", body)
	}
	if !strings.Contains(body, wantSub) {
		t.Fatalf("提示页缺少文案 %q:\n%s", wantSub, body)
	}
}

func TestSaveSiteSettingsShippingFeeRejectsInvalid(t *testing.T) {
	cases := []struct {
		name    string
		field   string
		value   string
		wantSub string
	}{
		{"基础运费负数", "shippingBaseFee", "-1", "基础运费"},
		{"基础运费非数字", "shippingBaseFee", "八元", "基础运费"},
		{"基础运费三位小数", "shippingBaseFee", "1.234", "基础运费"},
		{"基础运费超上限", "shippingBaseFee", "20000", "基础运费"},
		{"门槛负数", "shippingFreeThreshold", "-0.01", "满额免运费"},
		{"门槛非数字", "shippingFreeThreshold", "满100", "满额免运费"},
		{"门槛超上限", "shippingFreeThreshold", "10000.01", "满额免运费"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projects := &fakeShippingProjects{settings: json.RawMessage(`{}`)}
			rec := postSiteSettingsSave(t, projects, shippingSaveForm(map[string]string{
				tc.field: tc.value,
			}))
			assertJumpErr(t, rec, tc.wantSub)
			if projects.updates != 0 {
				t.Fatal("非法值不得落库（更不得静默归零）")
			}
		})
	}
}

func TestSaveSiteSettingsShippingZeroDeletesKeys(t *testing.T) {
	projects := &fakeShippingProjects{settings: json.RawMessage(`{"shippingBaseFee":800,"shippingFreeThreshold":10000}`)}
	rec := postSiteSettingsSave(t, projects, shippingSaveForm(map[string]string{
		"shippingBaseFee":       "0",
		"shippingFreeThreshold": "",
	}))
	assertJumpOK(t, rec)
	obj := settingsOf(t, projects)
	for _, key := range []string{"shippingBaseFee", "shippingFreeThreshold"} {
		if _, ok := obj[key]; ok {
			t.Errorf("清空后 %s 仍留在 settings 里（0 与缺键语义相同，不该留噪声）", key)
		}
	}
}

func TestSaveSiteSettingsShippingKeepsOtherKeys(t *testing.T) {
	projects := &fakeShippingProjects{
		settings: json.RawMessage(`{"unknownKey":"别家模块写的","siteDesc":"旧简介"}`),
	}
	rec := postSiteSettingsSave(t, projects, shippingSaveForm(map[string]string{"shippingBaseFee": "8"}))
	assertJumpOK(t, rec)
	obj := settingsOf(t, projects)
	if _, ok := obj["unknownKey"]; !ok {
		t.Error("本页不认识的键被整份覆盖删掉了 —— 那种丢失在页面上看不出来")
	}
	if got := int64Of(t, obj, "shippingBaseFee"); got != 800 {
		t.Errorf("基础运费应落库 800 分，实际 %d", got)
	}
}

func TestSiteSettingsTemplateRendersShippingFields(t *testing.T) {
	data := siteSettingsScriptsData()
	data["HeadScripts"] = ""
	data["BodyScripts"] = ""
	data["ShippingBaseFeeYuan"] = "8.00"
	data["ShippingFreeThresholdYuan"] = "100.00"
	out := renderSiteSettings(t, data)

	if !strings.Contains(out, "</html>") {
		t.Fatalf("站点设置页未完整渲染（缺 </html>）:\n%s", out)
	}
	for _, want := range []string{
		`name="shippingBaseFee"`,
		`id="set-shipping-base-fee"`,
		`value="8.00"`,
		`name="shippingFreeThreshold"`,
		`id="set-shipping-free-threshold"`,
		`value="100.00"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果缺少 %q —— 表单里没有这个输入项（或回显断了），保存时该字段永远是空", want)
		}
	}
}

func TestSiteSettingsWriteKeysAreRegistered(t *testing.T) {

	if projectenums.ErrGA4IDInvalid != "ErrGA4IDInvalid" || projectenums.ErrProjectInternal != "ErrProjectInternal" {
		t.Fatal("enums 常量值不再等于常量名：本判据的前提失效，需改为「名字 → 值」的显式映射")
	}
	src, err := os.ReadFile("project_page.go")
	if err != nil {
		t.Fatalf("读取站点设置页源码失败（路径假设变了要同步改）: %v", err)
	}
	re := regexp.MustCompile(`projectMsgOf\(projectenums\.([A-Za-z0-9_]+),`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatal("没有从源码里提取到任何受控文案出口：判据的空转形态（正则或调用形态变了）")
	}
	registered := make(map[string]bool, len(projectWriteTextKeys))
	for _, k := range projectWriteTextKeys {
		registered[k] = true
	}
	for _, m := range matches {
		if !registered[m[1]] {
			t.Errorf("写侧出口 %s 不在 projectWriteTextKeys 里：key 没有词条时页面会显示裸 key（不报错）", m[1])
		}
	}

	for _, key := range []string{projectenums.ErrShippingBaseFeeInvalid, projectenums.ErrShippingFreeThresholdInvalid} {
		if !registered[key] {
			t.Errorf("运费提示 %s 未登记进 projectWriteTextKeys", key)
		}
	}
}

func TestShippingPolicyKeysMatchStore(t *testing.T) {
	var parsed projectdto.SiteSettings
	if err := json.Unmarshal([]byte(`{"shippingBaseFee":800,"shippingFreeThreshold":10000}`), &parsed); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if parsed.ShippingBaseFee != 800 || parsed.ShippingFreeThreshold != 10000 {
		t.Fatalf("json tag 与存储键名不一致：%+v", parsed)
	}

	policy, perr := projectdto.NormalizeShippingPolicy(parsed.ShippingBaseFee, parsed.ShippingFreeThreshold)
	if perr != nil {
		t.Fatalf("合法值应通过：%v", perr)
	}
	if policy.BaseFeeCents != 800 || policy.FreeThresholdCents != 10000 {
		t.Fatalf("归一化改动了金额：%+v", policy)
	}
}
