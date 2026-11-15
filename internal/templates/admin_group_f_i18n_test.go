package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/i18n"
)

const groupFSeedFile = "../../public/schema/tables/sys_i18n.sql"

var groupFTemplates = []string{
	"admin/dashboard.html",
	"admin/login.html",
	"admin/system/administrators.html",
	"admin/system/roles.html",

	"admin/system/permissions.html",
	"admin/system/permission_edit_drawer.html",

	"admin/system/departments.html",
	"admin/system/i18n.html",
	"admin/masterdata/masterdata_changes.html",
	"admin/system/datarules.html",
	"admin/system/datarule_edit.html",

	"admin/system/datarule_config_editor.html",
	"admin/analytics/analytics.html",
}

// 三种真实取词形态都要认：全局 t(...)、数据键 .["t"](...)、别名 tr(...)。
// 只认后两种会让改用全局 t 的模板被判成「词条与模板失去同步」。
var groupFCallRe = regexp.MustCompile(`(?:\.\["t"\]|\bt(?:r|f|n)?)\("([^"]+)",\s*"((?:[^"\\]|\\.)*)"`)

var groupFSeedRe = regexp.MustCompile(`'([A-Za-z0-9_.]+)', '(zh-CN|en-US)'`)

func groupFTemplateKeys(t *testing.T) map[string]string {
	t.Helper()
	keys := make(map[string]string)
	for _, name := range groupFTemplates {
		src, err := os.ReadFile(filepath.FromSlash(name))
		if err != nil {
			t.Fatalf("读取模板 %s 失败: %v", name, err)
		}
		for _, m := range groupFCallRe.FindAllStringSubmatch(string(src), -1) {
			key, fallback := m[1], m[2]
			if prev, ok := keys[key]; ok && prev != fallback {
				t.Errorf("同一 key 出现两种兜底文案：%s = %q / %q", key, prev, fallback)
			}
			keys[key] = fallback
		}
	}
	if len(keys) == 0 {
		t.Fatal("未从本批模板提取到任何 t() 调用（模板清单或正则过时）")
	}
	return keys
}

func groupFSeedLangs(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	src, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("读取 seed %s 失败: %v", path, err)
	}
	langs := make(map[string]map[string]bool)
	for _, m := range groupFSeedRe.FindAllStringSubmatch(string(src), -1) {
		key, lang := m[1], m[2]
		if langs[key] == nil {
			langs[key] = map[string]bool{}
		}
		langs[key][lang] = true
	}
	if len(langs) == 0 {
		t.Fatalf("未从 %s 提取到任何词条（文件名或格式过时）", path)
	}
	return langs
}

func TestGroupFI18nTemplateKeysMatchSeed(t *testing.T) {
	keys := groupFTemplateKeys(t)
	seeded := groupFSeedLangs(t, groupFSeedFile)

	for key := range keys {
		langs := seeded[key]
		if langs == nil {
			t.Errorf("模板取词 %q 在 public/schema 里没有 seed（英文界面会回落中文）", key)
			continue
		}
		if !langs["zh-CN"] || !langs["en-US"] {
			t.Errorf("词条 %q 语言不成对：zh-CN=%v en-US=%v", key, langs["zh-CN"], langs["en-US"])
		}
	}
}

func TestGroupFI18nFallbacksArePlainText(t *testing.T) {
	for key, fallback := range groupFTemplateKeys(t) {
		if i := strings.IndexAny(fallback, "<>"); i >= 0 {
			t.Errorf("兜底文案含标签字符 %q：%s = %q", string(fallback[i]), key, fallback)
		}
		if strings.Contains(fallback, "|raw") || strings.Contains(fallback, "| raw") {
			t.Errorf("兜底文案出现 raw 过滤器痕迹：%s = %q", key, fallback)
		}
	}
}

func TestGroupFI18nTemplatesParse(t *testing.T) {
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)
	for _, name := range groupFTemplates {
		name := name
		t.Run(name, func(t *testing.T) {
			if _, err := set.GetTemplate(name); err != nil {
				t.Fatalf("模板解析失败：%v", err)
			}
		})
	}
}

func TestGroupFDashboardRenders(t *testing.T) {
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)

	base := map[string]any{
		"title": "仪表盘", "lang": "zh-CN", "langs": LanguageOptions("zh-CN"),
		"csrf_token": "tok", "HasSubnav": true, "SidebarPinned": true,
		"t": TranslateFunc("zh-CN"),

		"ProjectCount": 1, "PageTotal": 3, "PagePublished": 2, "PageDraft": 1, "PageStale": 1,
		"RecentPages": []map[string]any{{
			"ID": "p1", "Project": "官网", "Path": "/about", "Kind": "page",
			"Published": true, "Stale": true, "Version": 4, "UpdatedAt": "2026-09-18 09:00:00",
		}},
	}
	out, err := render(t, set, "admin/dashboard", base)
	if err != nil {
		t.Fatalf("仪表盘渲染失败: %v", err)
	}

	for _, want := range []string{
		"仪表盘", "问 AI（经营数据）", "全部会话", "发送", "停止", "思考过程",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("仪表盘缺少 %q（模板可能中途中断）", want)
		}
	}

	en := map[string]string{
		"admin.dashboard.title":    "Dashboard",
		"admin.dashboard.ai.title": "Ask AI (business data)",
	}
	enData := map[string]any{}
	for k, v := range base {
		enData[k] = v
	}
	enData["lang"] = "en-US"

	enData["title"] = "Dashboard"
	enData["t"] = func(key, fallback string) string {
		if v, ok := en[key]; ok {
			return v
		}
		return fallback
	}
	out, err = render(t, set, "admin/dashboard", enData)
	if err != nil {
		t.Fatalf("仪表盘英文渲染失败: %v", err)
	}
	for _, want := range []string{"Dashboard", "Ask AI (business data)"} {
		if !strings.Contains(out, want) {
			t.Errorf("仪表盘英文渲染缺少 %q", want)
		}
	}

	if strings.Contains(out, "仪表盘") {
		t.Error("注入英文翻译后不应再出现中文标题")
	}
}

func TestGroupFRangeTranslatePath(t *testing.T) {
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	injectGlobals(set)

	type adminRow struct {
		ID       int
		Username string
		Name     string
		Email    string
		Phone    string
		Status   int
	}
	shell := map[string]any{
		"lang": "zh-CN", "langs": LanguageOptions("zh-CN"), "csrf_token": "tok",
		"HasSubnav": true, "SidebarPinned": true, "t": TranslateFunc("zh-CN"),
	}
	admins := map[string]any{}
	for k, v := range shell {
		admins[k] = v
	}
	admins["title"] = "管理员列表"
	admins["Total"] = 2
	admins["Buttons"] = map[string]any{"admin.create": true, "admin.edit": true, "admin.delete": true}
	admins["Rows"] = []adminRow{
		{ID: 1, Username: "admin", Name: "甲", Email: "a@example.com", Phone: "138", Status: 1},
		{ID: 2, Username: "editor01", Status: 3},
	}
	out, err := render(t, set, "admin/system/administrators", admins)
	if err != nil {
		t.Fatalf("管理员列表渲染失败: %v", err)
	}

	for _, want := range []string{
		"管理员列表（共", "编辑管理员：admin", "编辑管理员：editor01",
		"启用", "封禁", "确认删除？", "创建管理员", "保存修改",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("管理员列表缺少 %q（range 可能在 tr 取词处中断）", want)
		}
	}
	if strings.Contains(out, "tr(") || strings.Contains(out, "{{") {
		t.Error("输出里残留模板语法字面量")
	}

	i18nData := map[string]any{}
	for k, v := range shell {
		i18nData[k] = v
	}
	i18nData["title"] = "文案词条"
	i18nData["Buttons"] = map[string]any{"i18n.manage": true}
	i18nData["Keyword"] = ""
	i18nData["LangFilter"] = ""
	i18nData["CatFilter"] = ""
	i18nData["Saved"] = ""
	i18nData["Errored"] = ""
	i18nData["Categories"] = []string{"ui", "shell"}
	i18nData["Entries"] = []i18n.Entry{{
		Key: "site.component.gallery.prev", Lang: "en-US", Value: "Previous",
		Category: "ui", UpdateTime: "2026-09-14 10:00",
	}}
	i18nData["I18nEditURLs"] = []string{"/admin/i18n/edit?key=site.component.gallery.prev&lang=en-US"}
	out, err = render(t, set, "admin/system/i18n", i18nData, groupDRenderScope())
	if err != nil {
		t.Fatalf("词条页渲染失败: %v", err)
	}
	for _, want := range []string{
		"文案词条", "site.component.gallery.prev", "编辑", "删除",
		`hx-get="/admin/i18n/edit?`, "tpl-i18n-create", "来源或修改原因",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("词条页缺少 %q（range 可能在 tr 取词处中断）", want)
		}
	}
}
