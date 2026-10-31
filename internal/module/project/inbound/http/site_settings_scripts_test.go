package projecthttp

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"

	"github.com/CloudyKit/jet/v6"
)

const siteSettingsScriptsTemplateDir = "../../../../templates"

const siteSettingsScriptsSample = "<script async src=\"https://cdn.example.com/tag.js\"></script>"

func renderSiteSettings(t *testing.T, data map[string]any) string {
	t.Helper()
	loader := jet.NewOSFileSystemLoader(siteSettingsScriptsTemplateDir)
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	tpl, err := set.GetTemplate("admin/project/settings")
	if err != nil {
		t.Fatalf("站点设置页模板解析失败: %v", err)
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, nil, data); err != nil {
		t.Fatalf("站点设置页渲染失败: %v", err)
	}
	return sb.String()
}

func siteSettingsScriptsData() map[string]any {
	return map[string]any{
		"lang": "zh-CN", "title": "站点设置", "menu": "settings", "csrf_token": "tok",

		"t": func(_, fallback string) string { return fallback },

		"Projects":                  []map[string]any{{"ID": "p1", "Name": "站点"}},
		"Selected":                  "p1",
		"Name":                      "站点",
		"SiteName":                  "站点显示名",
		"SiteDesc":                  "",
		"ContactEmail":              "",
		"GA4MeasurementID":          "",
		"SearchConsoleVerification": "",
		"IndexNowKey":               "",
		"NotFoundHTML":              "",
		"URLPatterns":               []map[string]any{},

		"ShippingBaseFeeYuan":       "",
		"ShippingFreeThresholdYuan": "",
		"Locales":                   []map[string]any{},
		"LocaleError":               "",
		"LocaleSaved":               false,
		"LangURLOffWarning":         false,
		"LangURLMode":               "off",
	}
}

func TestSiteSettingsTemplateRendersSiteScriptsFields(t *testing.T) {
	data := siteSettingsScriptsData()
	data["HeadScripts"] = ""
	data["BodyScripts"] = ""
	out := renderSiteSettings(t, data)

	if !strings.Contains(out, "</html>") {
		t.Fatalf("站点设置页未完整渲染（缺 </html>）:\n%s", out)
	}
	for _, want := range []string{
		`name="headScripts"`,
		`id="set-head-scripts"`,
		`maxlength="16384"`,
		`name="bodyScripts"`,
		`id="set-body-scripts"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("渲染结果缺少 %q —— 表单里没有这个输入项，保存时该字段永远是空", want)
		}
	}
}

func TestSiteSettingsTemplateRendersSiteScriptsEcho(t *testing.T) {
	evil := "</textarea><p>ESCAPED-MARK</p>"
	data := siteSettingsScriptsData()
	data["HeadScripts"] = siteSettingsScriptsSample
	data["BodyScripts"] = evil
	out := renderSiteSettings(t, data)

	if !strings.Contains(out, "cdn.example.com/tag.js") {
		t.Errorf("已配置的 Head 代码未回显到表单")
	}
	if strings.Contains(out, "ESCAPED-MARK</p>") {
		t.Errorf("textarea 内容被原样输出（Jet 转义失效）：`</textarea>` 会提前闭合输入框")
	}
	if !strings.Contains(out, "&lt;p&gt;ESCAPED-MARK&lt;/p&gt;") {
		t.Errorf("textarea 内容未以转义形式回显（用户会看不到自己填的内容）")
	}
}

func TestSiteSettingsTemplateRendersScriptsErrorSlot(t *testing.T) {
	data := siteSettingsScriptsData()
	data["HeadScripts"] = siteSettingsScriptsSample
	data["BodyScripts"] = ""
	data["HeadScriptsError"] = "admin.settings.scripts.head_invalid"
	out := renderSiteSettings(t, data)

	if !strings.Contains(out, "badge-warning") {
		t.Errorf("校验失败没有渲染出提示条")
	}

	if strings.Contains(out, "admin.settings.scripts.head_invalid") {
		t.Errorf("页面上出现了裸 i18n key（兜底链失效）")
	}
	if !strings.Contains(out, "结构性标签") {
		t.Errorf("提示文案未渲染（应显示调用点的中文兜底）")
	}
	if !strings.Contains(out, "cdn.example.com/tag.js") {
		t.Errorf("失败回渲染丢了用户输入 —— 他刚贴进去的脚本没了")
	}
}

func TestSiteSettingsTemplateRendersWithoutSiteScriptsKeys(t *testing.T) {
	out := renderSiteSettings(t, siteSettingsScriptsData())
	if !strings.Contains(out, "</html>") {
		t.Fatalf("缺可选键时站点设置页渲染失败（可选键必须用 isset 包裹）:\n%s", out)
	}
	if !strings.Contains(out, `name="headScripts"`) {
		t.Errorf("缺键时表单应渲染成空输入框，实际整块缺失")
	}
}

func TestMergeSiteSettingsSiteScripts(t *testing.T) {
	raw := json.RawMessage(`{"siteName":"旧站名","indexNowKey":"keep-me","unknownKey":"别家模块写的"}`)
	got, err := mergeSiteSettings(raw, projectcontract.SiteSettings{
		SiteName:    "旧站名",
		IndexNowKey: "keep-me",
		HeadScripts: siteSettingsScriptsSample,
		BodyScripts: "",
	})
	if err != nil {
		t.Fatalf("mergeSiteSettings: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("合并结果不是 JSON 对象: %v", err)
	}
	var head string
	if err := json.Unmarshal(obj["headScripts"], &head); err != nil {
		t.Fatalf("headScripts 未写入: %v", err)
	}
	if head != siteSettingsScriptsSample {
		t.Errorf("headScripts 存的值被改写: %q", head)
	}
	if _, ok := obj["bodyScripts"]; ok {
		t.Errorf("空的 Body 代码应当删除该键（清空 = 停止注入），实际留下了空值")
	}
	if _, ok := obj["unknownKey"]; !ok {
		t.Errorf("本页不认识的键被整份覆盖删掉了 —— 那种丢失在页面上看不出来")
	}
	if _, ok := obj["indexNowKey"]; !ok {
		t.Errorf("本页管的其它键被误删")
	}
}
