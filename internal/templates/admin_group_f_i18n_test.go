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

const groupFSeedFile = "../../public/migrations/193_i18n_seed_admin_system.sql"

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

var groupFRetiredKeys = map[string]bool{

	"admin.roles.perm.back": true,

	"admin.i18n.pager.total_pre":  true,
	"admin.i18n.pager.total_post": true,
	"admin.i18n.pager.page_post":  true,
	"admin.i18n.form.title":       true,

	"admin.i18n.saved":   true,
	"admin.i18n.unsaved": true,

	"admin.masterdata.rows.headingClose":     true,
	"admin.masterdata.entities.headingClose": true,

	"admin.masterdata.filter.title":  true,
	"admin.masterdata.current.title": true,

	"admin.masterdata.rows.heading":     true,
	"admin.masterdata.entities.heading": true,

	"admin.dashboard.action.cancel": true, "admin.dashboard.action.delete": true, "admin.dashboard.action.edit": true,
	"admin.dashboard.action.export": true, "admin.dashboard.action.new_admin": true, "admin.dashboard.action.save": true,
	"admin.dashboard.badge.draft": true, "admin.dashboard.badge.pending_review": true, "admin.dashboard.badge.published": true,
	"admin.dashboard.badge.rejected": true, "admin.dashboard.badges.title": true, "admin.dashboard.buttons.small": true,
	"admin.dashboard.buttons.title": true, "admin.dashboard.checkbox.welcome_mail": true, "admin.dashboard.col.dept": true,
	"admin.dashboard.col.last_login": true, "admin.dashboard.col.name": true, "admin.dashboard.col.role": true,
	"admin.dashboard.col.username": true, "admin.dashboard.dept.content": true, "admin.dashboard.dept.market": true,
	"admin.dashboard.dept.tech": true, "admin.dashboard.field.dept": true, "admin.dashboard.field.email": true,
	"admin.dashboard.field.name": true, "admin.dashboard.field.remark": true, "admin.dashboard.field.role": true,
	"admin.dashboard.field.status": true, "admin.dashboard.field.username": true, "admin.dashboard.form.required_hint": true,
	"admin.dashboard.health.degraded": true, "admin.dashboard.health.down": true, "admin.dashboard.health.ok": true,
	"admin.dashboard.health.unknown": true, "admin.dashboard.hint.username": true, "admin.dashboard.list.title": true,
	"admin.dashboard.option.dept_placeholder": true, "admin.dashboard.option.role_placeholder": true, "admin.dashboard.pager.next": true,
	"admin.dashboard.pager.prev": true, "admin.dashboard.pager.summary": true, "admin.dashboard.ph.name": true,
	"admin.dashboard.ph.remark": true, "admin.dashboard.ph.username": true, "admin.dashboard.role.developer": true,
	"admin.dashboard.role.editor": true, "admin.dashboard.role.super_admin": true, "admin.dashboard.role.viewer": true,
	"admin.dashboard.row.name_li": true, "admin.dashboard.row.name_wang": true, "admin.dashboard.row.name_zhang": true,
	"admin.dashboard.row.never_login": true, "admin.dashboard.stat.admins": true, "admin.dashboard.stat.admins_delta": true,
	"admin.dashboard.stat.online": true, "admin.dashboard.stat.online_note": true, "admin.dashboard.stat.pending": true,
	"admin.dashboard.stat.pending_note": true, "admin.dashboard.stat.published_note": true, "admin.dashboard.status.disabled": true,
	"admin.dashboard.status.disabled_row": true, "admin.dashboard.status.enabled": true, "admin.dashboard.status.pending": true,
	"admin.dashboard.subtitle": true,

	"admin.dashboard.stat.published": true,
	"admin.dashboard.col.status":     true,
	"admin.dashboard.col.actions":    true,
}

// 三种真实取词形态都要认：全局 t(...)、数据键 .["t"](...)、别名 tr(...)。
// 只认后两种会让改用全局 t 的模板被判成「词条与模板失去同步」。
var groupFCallRe = regexp.MustCompile(`(?:\.\["t"\]|\bt(?:r|f|n)?)\("([^"]+)",\s*"((?:[^"\\]|\\.)*)"`)

var groupFSeedRe = regexp.MustCompile(`\('([A-Za-z0-9_.]+)', '(zh-CN|en-US)'`)

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

func groupFAllSeedLangs(t *testing.T) map[string]map[string]bool {
	t.Helper()
	paths, err := filepath.Glob(filepath.FromSlash("../../public/migrations/*.sql"))
	if err != nil {
		t.Fatalf("列举迁移失败: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("未找到任何迁移 SQL（相对路径过时）")
	}
	all := make(map[string]map[string]bool)
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		for _, m := range groupFSeedRe.FindAllStringSubmatch(string(src), -1) {
			key, lang := m[1], m[2]
			if all[key] == nil {
				all[key] = map[string]bool{}
			}
			all[key][lang] = true
		}
	}
	return all
}

func TestGroupFI18nTemplateKeysMatchSeed(t *testing.T) {
	keys := groupFTemplateKeys(t)
	seededF := groupFSeedLangs(t, groupFSeedFile)
	seededAll := groupFAllSeedLangs(t)

	for key := range keys {
		langs, inBatch := seededF[key]
		src := filepath.Base(groupFSeedFile)
		if !inBatch {
			langs = seededAll[key]
			src = "public/migrations（既有迁移）"
		}
		if langs == nil {
			t.Errorf("模板取词 %q 在任何迁移中都没有 seed（英文界面会回落中文）", key)
			continue
		}
		if !langs["zh-CN"] || !langs["en-US"] {
			t.Errorf("词条 %q（%s）语言不成对：zh-CN=%v en-US=%v", key, src, langs["zh-CN"], langs["en-US"])
		}
	}

	for key := range seededF {
		if groupFRetiredKeys[key] {
			continue
		}
		if _, ok := keys[key]; !ok {
			t.Errorf("本批 seed 词条 %q 未被本批模板使用（词条与模板失去同步）", key)
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
