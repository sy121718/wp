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
	h := NewSiteSettingsAdminHandle(projects, nil)
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
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("合法保存应 303 回跳，实际 %d", rec.Code)
	}
	obj := settingsOf(t, projects)
	if got := int64Of(t, obj, "shippingBaseFee"); got != 800 {
		t.Errorf("8.00 元应落库为 800 分，实际 %d", got)
	}
	if got := int64Of(t, obj, "shippingFreeThreshold"); got != 10000 {
		t.Errorf("100 元门槛应落库为 10000 分，实际 %d", got)
	}
}

// TestSaveSiteSettingsShippingFeeRejectsInvalid 非法金额**不落库**，且提示指向出错的字段。
//
// 三类非法各有各的原因（负数 / 格式 / 超上限），但它们在页面上的出口是同一句受控文案；
// 断言点因此放在「**哪个字段**的提示」上：两个字段的文案必须是两条不同的 key，
// 否则用户对着两个几乎一样的输入框猜是哪一个填错了。
func TestSaveSiteSettingsShippingFeeRejectsInvalid(t *testing.T) {
	cases := []struct {
		name      string
		field     string
		value     string
		wantErrBy string // 期望的错误 key（读侧白名单里的那条）
	}{
		{"基础运费负数", "shippingBaseFee", "-1", projectenums.ErrShippingBaseFeeInvalid},
		{"基础运费非数字", "shippingBaseFee", "八元", projectenums.ErrShippingBaseFeeInvalid},
		{"基础运费三位小数", "shippingBaseFee", "1.234", projectenums.ErrShippingBaseFeeInvalid},
		{"基础运费超上限", "shippingBaseFee", "20000", projectenums.ErrShippingBaseFeeInvalid},
		{"门槛负数", "shippingFreeThreshold", "-0.01", projectenums.ErrShippingFreeThresholdInvalid},
		{"门槛非数字", "shippingFreeThreshold", "满100", projectenums.ErrShippingFreeThresholdInvalid},
		{"门槛超上限", "shippingFreeThreshold", "10000.01", projectenums.ErrShippingFreeThresholdInvalid},
	}
	var seen []string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projects := &fakeShippingProjects{settings: json.RawMessage(`{}`)}
			rec := postSiteSettingsSave(t, projects, shippingSaveForm(map[string]string{
				tc.field: tc.value,
			}))
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("非法值应 303 回带提示，实际 %d", rec.Code)
			}
			loc := rec.Header().Get("Location")
			if !strings.HasPrefix(loc, "/admin/settings?project=p1") {
				t.Fatalf("回跳地址丢失工程上下文: %q", loc)
			}
			// ?err= 的取值：无 i18n 资源时 TranslateMessage 返回 key 原文，
			// 有词条时返回当前语言译文 —— 两者都非空，且都必须是**这个字段**的那一条。
			parsed, perr := url.Parse(loc)
			if perr != nil {
				t.Fatalf("回跳地址不可解析: %v", perr)
			}
			got := parsed.Query().Get("err")
			if got == "" {
				t.Fatal("回带提示为空：用户看不到任何原因（写侧的翻译链断了）")
			}
			if !strings.Contains(got, tc.wantErrBy) {
				t.Fatalf("提示未指向 %s 字段那条 key（得到 %q）", tc.field, got)
			}
			if projects.updates != 0 {
				t.Fatal("非法值不得落库（更不得静默归零）")
			}
			seen = append(seen, got)
		})
	}
	// 两个字段各有各的提示：合成同一条就等于让用户自己猜是哪一个输入框填错了。
	if seen[0] == seen[4] {
		t.Fatalf("基础运费与门槛的提示相同（%q）：两个字段无法区分", seen[0])
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
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("清空应视为合法保存，实际 %d", rec.Code)
	}
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
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("应保存成功，实际 %d", rec.Code)
	}
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

// TestSiteSettingsWriteKeysAreWhitelisted 写侧产出的错误 key 必须全在读侧白名单里。
//
// 漏登记的后果是**静默的**：写侧 303 带上了提示，读侧 projectPageErrText 因为不在
// 白名单里把它判成伪造、落空串 —— 页面上什么都没有，日志里也没有。
// 判据从源码里提取（而不是手工列一遍），这样新增一条 TranslateMessage 就自动纳入守卫。
func TestSiteSettingsWriteKeysAreWhitelisted(t *testing.T) {
	// 判据依赖「本模块 enums 的常量值 == 常量名」这条既有约定（projectenums 头部明文如此）。
	// 先自检这条前提仍成立 —— 否则提取出的名字与白名单里的值对不上，断言会退化成永远通过。
	if projectenums.ErrGA4IDInvalid != "ErrGA4IDInvalid" || projectenums.ErrProjectInternal != "ErrProjectInternal" {
		t.Fatal("enums 常量值不再等于常量名：本判据的前提失效，需改为「名字 → 值」的显式映射")
	}
	src, err := os.ReadFile("site_settings_admin_pages.go")
	if err != nil {
		t.Fatalf("读取站点设置页源码失败（路径假设变了要同步改）: %v", err)
	}
	re := regexp.MustCompile(`response\.TranslateMessage\(c,\s*projectenums\.([A-Za-z0-9_]+)\)`)
	matches := re.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatal("没有从源码里提取到任何受控文案出口：判据的空转形态（正则或调用形态变了）")
	}
	registered := make(map[string]bool, len(projectPageErrKeys))
	for _, k := range projectPageErrKeys {
		registered[k] = true
	}
	for _, m := range matches {
		if !registered[m[1]] {
			t.Errorf("写侧出口 %s 不在 projectPageErrKeys 里：提示会被读侧判成伪造而落空串（不报错）", m[1])
		}
	}
	// 反向：运费那两条必须真的在金里面（正例，防判据整体失效）。
	for _, key := range []string{projectenums.ErrShippingBaseFeeInvalid, projectenums.ErrShippingFreeThresholdInvalid} {
		if !registered[key] {
			t.Errorf("运费提示 %s 未登记进白名单", key)
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
