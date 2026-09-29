package projecthttp

// site_settings_scripts_test.go — 站点设置页「自定义 Head / Body 代码」（PIPE-8）的
// 渲染与存储契约断言。
//
// 三件事，都只在这一层能验证：
//  1. **模板真的能整份渲染**（不是「源码里有这几个字符串」）—— 本页 extends layout.html，
//     新增字段若把 Jet 表达式写坏，症状是整块表单消失或整页 500（渲染器先渲到 buffer、
//     失败即丢弃半截内容），静态文本比对抓不到；
//  2. **回显与错误槽同时成立**：校验失败时就地回渲染必须把用户刚贴的脚本原样带回，
//     否则他的一段 Pixel 代码就白填了；
//  3. **存储合并的键语义**：写入、清空（删键）、以及「本页不认识的键原样保留」。
//
// 保存路径的形状判据（超长 / 结构性标签）在 internal/builder/site_scripts_test.go，
// 这里不重复它 —— 那条判据的唯一出口是 builder.NormalizeHeadScripts / NormalizeBodyScripts。

import (
	"encoding/json"
	"strings"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"

	"github.com/CloudyKit/jet/v6"
)

// siteSettingsScriptsTemplateDir 模板根目录（从本包出发定位 internal/templates）。
//
// 路径假设变了（模板根搬迁）要同步改这条 —— 找不到文件时用例直接 fatal，
// 不会退化成永远通过的空检查。
const siteSettingsScriptsTemplateDir = "../../../../templates"

const siteSettingsScriptsSample = "<script async src=\"https://cdn.example.com/tag.js\"></script>"

// renderSiteSettings 用与生产同一份模板文件渲染站点设置页（生产走 embed FS，内容同源）。
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

// siteSettingsScriptsData 本页模板所需的渲染数据。
//
// 键与 handle 的 templateMap 一一对应：模板只渲染、不查询，缺键就是渲染失败
// （可选键走 isset 的那几个例外见下面的单独用例）。
func siteSettingsScriptsData() map[string]any {
	return map[string]any{
		"lang": "zh-CN", "title": "站点设置", "menu": "settings", "csrf_token": "tok",
		// 取词函数：这里刻意用「直接返回兜底」的实现 —— 它正是词条未登记时的真实行为，
		// 断言因此同时钉住了「缺词条也不会显示裸 key」这条兜底链。
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
		// 运费规则（库内分 → 表单元）：模板用点号取值，缺键是渲染中断而不是零值。
		"ShippingBaseFeeYuan":       "",
		"ShippingFreeThresholdYuan": "",
		"Locales":                   []map[string]any{},
		"LocaleError":               "",
		"LocaleSaved":               false,
		"LangURLOffWarning":         false,
		"LangURLMode":               "off",
	}
}

// TestSiteSettingsTemplateRendersSiteScriptsFields 两个输入项真的出现在渲染结果里（整页闭合）。
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

// TestSiteSettingsTemplateRendersSiteScriptsEcho 已配置的脚本必须回显，且不破坏表单结构。
//
// 回显断了的表现是「打开设置页看到空白，以为没配过」——管理员会重贴一遍，或者更糟：
// 以为配置丢了。同时钉住 textarea 的内容**不是**原样 HTML（{{.HeadScripts}} 走 Jet 转义）：
// 未转义时 `</textarea>` 会提前闭合输入框，后面的整块表单会跑进页面正文。
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

// TestSiteSettingsTemplateRendersScriptsErrorSlot 校验失败时就地回渲染：错误提示与用户输入同时在场。
//
// 这是 PIPE-8 选定的失败出口（不回 303 + ?err=）：两个字段装的是几百字节脚本，
// 303 一跳表单就空了，用户刚贴的代码跟着丢。这条用例钉住「提示可见 + 输入还在」。
func TestSiteSettingsTemplateRendersScriptsErrorSlot(t *testing.T) {
	data := siteSettingsScriptsData()
	data["HeadScripts"] = siteSettingsScriptsSample
	data["BodyScripts"] = ""
	data["HeadScriptsError"] = "admin.settings.scripts.head_invalid"
	out := renderSiteSettings(t, data)

	if !strings.Contains(out, "badge-warning") {
		t.Errorf("校验失败没有渲染出提示条")
	}
	// 词条未登记时经 .["t"](key, 兜底) 回落调用点写的中文兜底 —— 页面绝不能显示裸 key。
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

// TestSiteSettingsTemplateRendersWithoutSiteScriptsKeys 数据里没有这四个键时页面照常渲染。
//
// 判据来自 internal/templates 的渲染助手（settingsRenderData）不提供这些键：
// 模板若写成裸 {{.HeadScripts}}，缺键会让整页渲染中断（半截内容被丢弃 + 500），
// 而那种红会出现在**别的包**里、与本改动看起来毫无关系。故这里显式钉住 isset 口径。
func TestSiteSettingsTemplateRendersWithoutSiteScriptsKeys(t *testing.T) {
	out := renderSiteSettings(t, siteSettingsScriptsData())
	if !strings.Contains(out, "</html>") {
		t.Fatalf("缺可选键时站点设置页渲染失败（可选键必须用 isset 包裹）:\n%s", out)
	}
	if !strings.Contains(out, `name="headScripts"`) {
		t.Errorf("缺键时表单应渲染成空输入框，实际整块缺失")
	}
}

// TestMergeSiteSettingsSiteScripts 合并语义：写入、清空删键、其它键原样保留。
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
