package projecthttp

// site_settings_shipping_test.go — 站点级运费设置（基础运费 + 满额免运费门槛）的
// 保存校验、元/分换算与页面渲染契约。
//
// 三件事只在这一层能验证：
//  1. **换算发生在表单边界**：库内是分、表单是元，且回显走同一对函数
//     （换算写错的表现是「每次保存金额都变一点」，没有任何报错）；
//  2. **非法值不落库、不静默归零**：负数 / 非数字 / 超上限一律 303 回带**对应字段**的提示，
//     那条提示还要能在读侧白名单里被接受（否则写侧发了、页面上什么都不显示）；
//  3. **模板真的能整份渲染**（不是「源码里有这几个字符串」）—— 本页 extends layout.html，
//     新字段把 Jet 表达式写坏的症状是整块表单消失或整页 500，静态文本比对抓不到。
//
// 金额复算（三档交互：收 / 免 / 不收）在 internal/module/cart/service/cart_shipping_test.go
// 与 public/test/cart/feature/cart_shipping_membership_test.go，这里只管设置面。

import (
	"context"
	"encoding/json"
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

// fakeShippingProjects 站点设置保存链路的工程契约替身。
//
// 嵌入接口做**部分桩**：本文件的用例只走 Detail / Update 两次调用，其余方法不会被调到
// （形态照 internal/module/user/inbound/http 的 fakeProjects）。
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

// postSiteSettingsSave 以给定表单提交站点设置保存（POST /admin/settings/save）。
func postSiteSettingsSave(t *testing.T, projects projectcontract.ProjectService, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 写动作的结论由 shell.RenderJump 渲染整页提示（HTTP 200），需要真实模板渲染器；
	// 旧形态（303）不需要，所以这里在改造时补上。
	router.HTMLRender = templates.NewJetHTMLRender(siteSettingsScriptsTemplateDir, true)
	h := NewSiteSettingsAdminHandle(projects, nil, nil)
	router.POST("/admin/settings/save", h.SaveSiteSettings)

	req := httptest.NewRequest(http.MethodPost, "/admin/settings/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// shippingSaveForm 站点设置保存的最小表单（工程名必须给：缺它连「回哪一页」都定不下来）。
func shippingSaveForm(extra map[string]string) url.Values {
	form := url.Values{"projectId": {"p1"}, "name": {"站点"}}
	for k, v := range extra {
		form.Set(k, v)
	}
	return form
}

// settingsOf 读替身最后写入的 settings JSON。
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

// int64Of 读 settings 里的整数键（键不存在返回 0）。
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

// TestSaveSiteSettingsShippingFeeToCents 表单的「元」落库成「分」。
//
// 这是本批金额口径的关键一环：库内与订单同口径（分），表单是元。写错方向的表现
// 是运费变成 100 倍或 1/100 —— 前者会让客户直接不买，后者让运费看起来「没生效」。
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

// assertJumpOK 断言响应是**整页成功提示**（HTTP 200 + data-jump-state="ok"）。
//
// 取代原先的「303 + Location 带 ?err= / ?ok=」：写动作的结论改由 shell.RenderJump
// 渲染整页提示（文案走响应体、不进 URL）。
func assertJumpOK(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("成功应 200 渲染提示页，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("响应不是成功提示页（缺 data-jump-state=\"ok\"）:\n%s", rec.Body.String())
	}
}

// assertJumpErr 断言响应是**整页失败提示**（HTTP 200 + data-jump-state="err"），且含给定文案。
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

// TestSaveSiteSettingsShippingFeeRejectsInvalid 非法金额**不落库**，且提示指向出错的字段。
//
// 三类非法各有各的原因（负数 / 格式 / 超上限），但它们在页面上的出口是同一句受控文案；
// 断言点因此放在「**哪个字段**的提示」上：两个字段的文案必须是两条不同的 key，
// 否则用户对着两个几乎一样的输入框猜是哪一个填错了。
func TestSaveSiteSettingsShippingFeeRejectsInvalid(t *testing.T) {
	cases := []struct {
		name    string
		field   string
		value   string
		wantSub string // 期望的错误文案里能区分字段的那一段（词条缺失时的中文兜底）
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

// TestSaveSiteSettingsShippingZeroDeletesKeys 0 = 关掉这一项，键在存储层被删掉。
//
// 「留空 / 填 0 = 不收运费」如果落成 `"shippingBaseFee": 0`，语义上等价（读侧解析成 0），
// 但会在 JSON 里留噪声，并让「清空 = 关掉」在存储层看起来没生效（运营会反复保存）。
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

// TestSaveSiteSettingsShippingKeepsOtherKeys 只动本页管的键：别的批次写进同一列的键必须原样保留。
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

// TestSiteSettingsTemplateRendersShippingFields 两个运费输入项真的出现在渲染结果里。
//
// 键名写错是这一页最容易犯又最难发现的错：表单照样渲染、保存照样 200，
// 只是那个字段永远存不进去（本页保存是「按字段逐个读表单」）。
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

// TestSiteSettingsWriteKeysAreRegistered 写侧产出的错误 key 必须全在 projectWriteTextKeys 里。
//
// 漏登记的后果是**静默的**：写侧渲染提示页时 key 没有词条，页面上显示的是**裸 key**
// （如 `ErrGA4IDInvalid`），不报错、不记日志。
// 判据从源码里提取（而不是手工列一遍），这样新增一条 projectText 就自动纳入守卫。
func TestSiteSettingsWriteKeysAreRegistered(t *testing.T) {
	// 判据依赖「本模块 enums 的常量值 == 常量名」这条既有约定（projectenums 头部明文如此）。
	// 先自检这条前提仍成立 —— 否则提取出的名字与登记表里的值对不上，断言会退化成永远通过。
	if projectenums.ErrGA4IDInvalid != "ErrGA4IDInvalid" || projectenums.ErrProjectInternal != "ErrProjectInternal" {
		t.Fatal("enums 常量值不再等于常量名：本判据的前提失效，需改为「名字 → 值」的显式映射")
	}
	src, err := os.ReadFile("project_page.go")
	if err != nil {
		t.Fatalf("读取站点设置页源码失败（路径假设变了要同步改）: %v", err)
	}
	re := regexp.MustCompile(`projectText\(c,\s*projectenums\.([A-Za-z0-9_]+),`)
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
	// 反向：运费那两条必须真的在登记表里（正例，防判据整体失效）。
	for _, key := range []string{projectenums.ErrShippingBaseFeeInvalid, projectenums.ErrShippingFreeThresholdInvalid} {
		if !registered[key] {
			t.Errorf("运费提示 %s 未登记进 projectWriteTextKeys", key)
		}
	}
}

// TestShippingPolicyKeysMatchStore 存储键名与 dto 的 json tag 一致（读写两侧的接缝）。
//
// 写侧 mergeSiteSettings 用的是字面量键名，读侧靠 dto 的 json tag 解析 —— 两处
// 拼错一个字母的表现是「保存 200、读出来永远是 0」，页面上看不出任何异常。
func TestShippingPolicyKeysMatchStore(t *testing.T) {
	var parsed projectdto.SiteSettings
	if err := json.Unmarshal([]byte(`{"shippingBaseFee":800,"shippingFreeThreshold":10000}`), &parsed); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if parsed.ShippingBaseFee != 800 || parsed.ShippingFreeThreshold != 10000 {
		t.Fatalf("json tag 与存储键名不一致：%+v", parsed)
	}
	// 归一化（保存与读取共用）不改动金额。
	policy, perr := projectdto.NormalizeShippingPolicy(parsed.ShippingBaseFee, parsed.ShippingFreeThreshold)
	if perr != nil {
		t.Fatalf("合法值应通过：%v", perr)
	}
	if policy.BaseFeeCents != 800 || policy.FreeThresholdCents != 10000 {
		t.Fatalf("归一化改动了金额：%+v", policy)
	}
}
