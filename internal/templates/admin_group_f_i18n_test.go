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

// admin_group_f_i18n_test.go — I18N-001 组F（系统管理类模板）的 key 化判据。
//
// 判据按**本批自己的 key 枚举**计数，不写总量：其余批次同时在改别的模板，
// 总量判据会被别人的行满足，本批漏配 seed 就会被静默跳过。
//
// 覆盖 11 个模板（layout.html 已于早期 shell.* 迁移中完成，本批未动）
// 与同批 seed 文件 193_i18n_seed_admin_system.sql。

// groupFSeedFile 本批 seed（相对 internal/templates）。
const groupFSeedFile = "../../public/migrations/193_i18n_seed_admin_system.sql"

// groupFTemplates 本批改动的模板。
var groupFTemplates = []string{
	"admin/dashboard.html",
	"admin/login.html",
	"admin/administrators.html",
	"admin/roles.html",
	"admin/permissions.html",
	"admin/departments.html",
	"admin/i18n.html",
	"admin/masterdata_changes.html",
	"admin/datarules.html",
	"admin/datarule_edit.html",
	"admin/analytics.html",
}

// groupFCallRe 匹配两种取词写法：{{ .["t"]("k","兜底") }} 与 range 内的 {{tr("k","兜底")}}。
var groupFCallRe = regexp.MustCompile(`(?:\.\["t"\]|\btr)\("([^"]+)",\s*"((?:[^"\\]|\\.)*)"\)`)

// groupFSeedRe 匹配 seed 里的 (item_key, lang) 对。
var groupFSeedRe = regexp.MustCompile(`\('([A-Za-z0-9_.]+)', '(zh-CN|en-US)'`)

// groupFTemplateKeys 提取本批模板里出现的全部 key 与兜底文案。
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

// groupFSeedLangs 提取 seed 文件里的 key → 语言集合。
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

// groupFAllSeedLangs 汇总 public/migrations 下全部 seed 的词条：
// 本批模板里有一部分取词（shell.*）在早前的迁移中已经 seed，
// 它们不属于本批新增，但同样必须中英成对 —— 判据按全部迁移看，才既能
// 抓「本批漏配」也能抓「复用了没 seed 的旧 key」。
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

// TestGroupFI18nTemplateKeysMatchSeed 双向校验：模板用到的 key 都在 seed 里且中英成对；
// seed 里的 key 也确实被本批模板用到（防止写了词条却没人取）。
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
	// 反向：本批 seed 必须恰好覆盖「模板新增的 key」，不能多也不能少。
	for key := range seededF {
		if _, ok := keys[key]; !ok {
			t.Errorf("本批 seed 词条 %q 未被本批模板使用（词条与模板失去同步）", key)
		}
	}
}

// TestGroupFI18nFallbacksArePlainText 兜底文案必须是纯文本。
//
// sys_i18n 里的词条后台可编辑，一旦允许标签就会把后台变成 HTML 注入通道；
// 且本批约定含 <strong> / <code> 的长句按标签边界拆 key，标签留在模板里。
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

// TestGroupFI18nTemplatesParse 本批模板必须能被 Jet 解析。
//
// 解析失败是整页级故障：{{* ... *}} 这类双大括号注释会让 Jet 报错，
// 而页面可能仍是 HTTP 200 + 空 body —— 浏览器里看不到任何线索。
func TestGroupFI18nTemplatesParse(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	for _, name := range groupFTemplates {
		name := name
		t.Run(name, func(t *testing.T) {
			if _, err := set.GetTemplate(name); err != nil {
				t.Fatalf("模板解析失败：%v", err)
			}
		})
	}
}

// TestGroupFDashboardRenders 渲染冒烟：静态演示页 dashboard 完整渲染，
// 且注入英文翻译后同一模板输出英文（证明取词路径真的接通了，而不是只改了字面量）。
func TestGroupFDashboardRenders(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))

	base := map[string]any{
		"title": "仪表盘", "lang": "zh-CN", "langs": LanguageOptions("zh-CN"),
		"csrf_token": "tok", "HasSubnav": true, "SidebarPinned": true,
		"t": TranslateFunc("zh-CN"),
	}
	out, err := render(t, set, "admin/dashboard", base)
	if err != nil {
		t.Fatalf("仪表盘渲染失败: %v", err)
	}
	for _, want := range []string{
		"仪表盘", "系统概览与组件演示", "导出", "新增管理员",
		"管理员总数", "已发布页面", "管理员列表", "最后登录",
		"共 128 条，第 1-4 条", "发送欢迎邮件", "徽章与状态", "小按钮",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("仪表盘缺少 %q（模板可能中途中断）", want)
		}
	}

	en := map[string]string{
		"admin.dashboard.title":         "Dashboard",
		"admin.dashboard.action.export": "Export",
		"admin.dashboard.col.actions":   "Actions",
		"admin.dashboard.buttons.small": "Small button",
	}
	enData := map[string]any{}
	for k, v := range base {
		enData[k] = v
	}
	enData["lang"] = "en-US"
	// layout 顶栏与 <title> 取的是 .title 数据（不是词条），英文渲染下也要换成英文，
	// 否则断言「全文无中文」会被它误伤。
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
	for _, want := range []string{"Dashboard", "Export", "Actions", "Small button"} {
		if !strings.Contains(out, want) {
			t.Errorf("仪表盘英文渲染缺少 %q", want)
		}
	}
	if strings.Contains(out, "仪表盘") || strings.Contains(out, "小按钮") {
		t.Error("注入英文翻译后不应再出现这些中文原文")
	}
}

// TestGroupFRangeTranslatePath 验证 range 内的取词写法真的渲染出词条。
//
// 这是本批最容易出错的一点：Jet 不在 range 内做 := 赋值，且 range 内 . 指向当前元素，
// 直接 {{ .["t"](...) }} 会作用在行数据上。模板因此在块顶存 {{tr := .["t"]}}，
// 再在 range 内调 tr(...)；若该路径不成立，页面不会 500 —— 它会在 range 处中断，
// 表格整块消失而状态码仍是 200。
func TestGroupFRangeTranslatePath(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))

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
	admins["PermSet"] = map[string]any{"admin:create": true, "admin:edit": true, "admin:delete": true}
	admins["Rows"] = []adminRow{
		{ID: 1, Username: "admin", Name: "甲", Email: "a@example.com", Phone: "138", Status: 1},
		{ID: 2, Username: "editor01", Status: 3},
	}
	out, err := render(t, set, "admin/administrators", admins)
	if err != nil {
		t.Fatalf("管理员列表渲染失败: %v", err)
	}
	// 尾部断言（第二个行抽屉）才说明 range 没有被中断。
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
	i18nData["Total"] = 1
	i18nData["Page"] = 1
	i18nData["Pages"] = 1
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
	out, err = render(t, set, "admin/i18n", i18nData)
	if err != nil {
		t.Fatalf("词条页渲染失败: %v", err)
	}
	for _, want := range []string{
		"文案词条", "共 1 条，第 1 / 1 页（每页 50 条，按 key 升序）",
		"site.component.gallery.prev", "编辑", "删除",
		"新增 / 编辑", "来源或修改原因",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("词条页缺少 %q（range 可能在 tr 取词处中断）", want)
		}
	}
}
