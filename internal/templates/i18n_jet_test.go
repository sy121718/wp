package templates

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/pkg/i18n"
)

// i18n_jet_test.go — Jet 模板层取翻译路径的实测依据（多语言 P1 第二步）。
//
// 本文件把选型结论固化为可执行断言：
//   - 采用：handler 注入 t 函数（数据 map 里的函数值 + {{ .["t"]("key","原文") }}）；
//   - 不采用：预翻译 map（缺 key 渲染空串；map 整体缺失直接运行时错误）；
//   - 不采用：Set 级 AddGlobal（Set 进程级共享，无按请求语言通道）。

// memSet 构造内存模板 Set（与生产 NewJetHTMLRender 同配置）。
func memSet(t *testing.T, files map[string]string) *jet.Set {
	t.Helper()
	l := jet.NewInMemLoader()
	for name, src := range files {
		l.Set(name, src)
	}
	return jet.NewSet(l, jet.WithTemplateNameExtensions([]string{"", ".html"}))
}

// render 渲染模板并返回输出。
// resolveAdminTemplateName 把 `admin/<短名>` 解析成模板分目录后的真实名字
// （`admin/product/product_brands.html`）。已经是 `admin/<模块>/<名字>`，或非 admin/ 前缀的
// （fragments/ site/）一律原样返回。
//
// 为什么放在 render 里而不是改一片调用点：调用点只该关心「哪个页面」，不该知道文件被搬去了
// 哪个子目录 —— 否则每搬一次目录就是一片 `template not found` 的红，而那些红与断言无关，
// 最容易被当成环境问题糊过去。
func resolveAdminTemplateName(t *testing.T, name string) string {
	t.Helper()
	const p = "admin/"
	if !strings.HasPrefix(name, p) {
		return name
	}
	rel := strings.TrimPrefix(name, p)
	base := filepath.Base(rel)
	if !strings.HasSuffix(base, ".html") {
		base += ".html"
	}
	// 已经是 <模块>/<名字> 且那个文件真的在 → 原样返回；否则（含已经搬走的
	// admin/partials/<名字>，片段随模块目录调整过）按 basename 递归找。
	if strings.Contains(rel, "/") {
		if _, err := os.Stat(filepath.Join("admin", filepath.FromSlash(rel))); err == nil {
			return name
		}
	}
	for _, f := range adminTemplateFiles(t) {
		if filepath.Base(f) == base {
			return p + filepath.ToSlash(f[len("admin/"):])
		}
	}
	return name // 找不到就原样返回：让 set.GetTemplate 报它自己的错，不在这里吞掉
}

func render(t *testing.T, set *jet.Set, name string, data any) (string, error) {
	t.Helper()
	name = resolveAdminTemplateName(t, name)
	tmpl, err := set.GetTemplate(name)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, nil, data)
	return buf.String(), err
}

// TestTranslateFuncFallbackChain 验证翻译函数的四级兜底（i18n 未初始化时即走原文兜底，绝不报错）。
func TestTranslateFuncFallbackChain(t *testing.T) {
	// 未初始化 i18n 缓存（测试进程内不连库）：key 查不到 → 返回模板内中文原文
	tf := TranslateFunc("en-US")
	if got := tf("shell.brand", "管理后台"); got != "管理后台" {
		t.Fatalf("缺 key 应回退原文，实际 %q", got)
	}
	// fallback 为空 → 返回 key 本身，保证不输出空串
	if got := tf("shell.unknown.key", ""); got != "shell.unknown.key" {
		t.Fatalf("fallback 为空应返回 key 本身，实际 %q", got)
	}
	// key 为空 → 返回 fallback（模板误传空 key 时不报错）
	if got := tf("", "原文"); got != "原文" {
		t.Fatalf("空 key 应返回 fallback，实际 %q", got)
	}
	// lang 为空 → pkg/i18n 用默认语言，仍未命中则回退原文
	if got := TranslateFunc("")("shell.brand", "管理后台"); got != "管理后台" {
		t.Fatalf("空 lang 应回退原文，实际 %q", got)
	}
}

// TestLanguageOptions 验证语言下拉选项与当前语言标记。
func TestLanguageOptions(t *testing.T) {
	opts := LanguageOptions("en-US")
	if len(opts) != 2 || opts[0].Code != "zh-CN" || opts[1].Code != "en-US" {
		t.Fatalf("选项应固定为 zh-CN/en-US，实际 %+v", opts)
	}
	if opts[0].Active || !opts[1].Active {
		t.Fatalf("当前语言应为 en-US，实际 %+v", opts)
	}
	// 未匹配（含非法语言）时选中第一项，不报错
	opts = LanguageOptions("ja-JP")
	if !opts[0].Active || opts[1].Active {
		t.Fatalf("未匹配语言应选中第一项，实际 %+v", opts)
	}
}

// TestJetTranslateFuncPath 验证选定路径：根模板、{{include}} 片段、{{import}}+{{yield}} 块内均可用。
func TestJetTranslateFuncPath(t *testing.T) {
	set := memSet(t, map[string]string{
		"root.html": "{{import \"blk.html\"}}ROOT[{{ .[\"t\"](\"k\", \"兜底\") }}]" +
			"{{include \"inc.html\"}}{{yield blk(nodes=.Nodes, depth=0)}}",
		"inc.html": "INC[{{ .[\"t\"](\"k\", \"兜底\") }}]",
		"blk.html": "{{block blk(nodes, depth)}}BLK[{{ .[\"t\"](\"k\", \"兜底\") }}][len={{len(nodes)}}]{{end}}",
	})
	data := map[string]any{
		"t":     func(key, fallback string) string { return "T(" + fallback + ")" },
		"Nodes": []map[string]any{{"Title": "n1"}},
	}
	out, err := render(t, set, "root.html", data)
	if err != nil {
		t.Fatalf("函数值路径应渲染成功，实际 %v", err)
	}
	want := "ROOT[T(兜底)]INC[T(兜底)]BLK[T(兜底)][len=1]"
	if out != want {
		t.Fatalf("函数值路径输出 = %q，want %q", out, want)
	}

	// 未注入 t（例如 handler 漏调 withI18n）时渲染空串而非报错：绝不 500。
	out, err = render(t, set, "root.html", map[string]any{"Nodes": []map[string]any{}})
	if err != nil {
		t.Fatalf("缺 t 时不应报错，实际 %v", err)
	}
	if strings.Contains(out, "T(") {
		t.Fatalf("缺 t 时不应有翻译输出，实际 %q", out)
	}
}

// TestJetPreTranslatedMapIsFragile 记录未采用「预翻译 map」的实测原因。
func TestJetPreTranslatedMapIsFragile(t *testing.T) {
	set := memSet(t, map[string]string{
		"b.html": "B[{{ .[\"i18n\"][\"k\"] }}][{{ .[\"i18n\"][\"missing\"] }}]",
	})
	out, err := render(t, set, "b.html", map[string]any{"i18n": map[string]string{"k": "已翻译"}})
	if err != nil {
		t.Fatalf("map 路径渲染不应报错，实际 %v", err)
	}
	// 缺 key 渲染空串：无法回退原文（模板内拿不到中文原文）
	if out != "B[已翻译][]" {
		t.Fatalf("预翻译 map 缺 key 应为空串，实际 %q", out)
	}

	// i18n map 整体缺失 → Jet 运行时错误（漏注入即 500）
	_, err = render(t, set, "b.html", map[string]any{})
	if err == nil {
		t.Fatal("i18n map 整体缺失时应为运行时错误（这是放弃该路径的原因）")
	}
	if !strings.Contains(err.Error(), "invalid reflect.Value") {
		t.Fatalf("预期 invalid reflect.Value 运行时错误，实际 %v", err)
	}
}

// TestJetSetGlobalIsProcessShared 记录未采用「Set 级 AddGlobal」的实测原因。
//
// Set 由 NewJetHTMLRender 在启动时创建并被所有请求共享（进程级单例），
// 全局函数没有请求上下文：按请求切语言只能改共享状态（-race 下报 DATA RACE）。
func TestJetSetGlobalIsProcessShared(t *testing.T) {
	set := memSet(t, map[string]string{
		"c.html":   "C[{{ t(\"k\", \"兜底\") }}]",
		"ctx.html": "{{ ctx() }}",
	})

	set.AddGlobal("t", func(key, fallback string) string { return "zh-CN" })
	out, err := render(t, set, "c.html", map[string]any{})
	if err != nil {
		t.Fatalf("AddGlobal 渲染不应报错，实际 %v", err)
	}
	if out != "C[zh-CN]" {
		t.Fatalf("AddGlobal 输出 = %q，want C[zh-CN]", out)
	}

	// 第二个「请求」切语言：改写的是同一个 Set 的全局值，所有渲染共享（无请求隔离）
	set.AddGlobal("t", func(key, fallback string) string { return "en-US" })
	out, err = render(t, set, "c.html", map[string]any{})
	if err != nil {
		t.Fatalf("AddGlobal 渲染不应报错，实际 %v", err)
	}
	if out != "C[en-US]" {
		t.Fatalf("AddGlobal 共享状态应被改写，实际 %q", out)
	}
	if _, found := set.LookupGlobal("t"); !found {
		t.Fatal("AddGlobal 写入的是 Set 全局表（进程级共享）")
	}

	// 全局函数唯一的按渲染通道是 Runtime.Context()（池化字段，且会被 include/exec 的
	// 上下文参数覆盖），依赖它取请求语言过于脆弱，故不作为方案。
	var seen any
	set.AddGlobal("ctx", jet.Func(func(a jet.Arguments) reflect.Value {
		seen = a.Runtime().Context().Interface()
		return reflect.ValueOf("")
	}))
	if _, err := render(t, set, "ctx.html", map[string]any{"lang": "zh-CN"}); err != nil {
		t.Fatalf("Runtime.Context 探针不应报错，实际 %v", err)
	}
	if m, ok := seen.(map[string]any); !ok || m["lang"] != "zh-CN" {
		t.Fatalf("Runtime.Context 指向渲染数据，实际 %#v", seen)
	}
}

// TestAdminShellTemplatesRender 用真实后台外壳模板验证接入形态：
//  1. 无 i18n 数据时渲染中文原文（兜底不报错）；
//  2. 注入英文翻译函数后，同一模板输出英文（证明文案确实随语言变化）。
func TestAdminShellTemplatesRender(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))

	base := map[string]any{
		"lang":          "en-US",
		"langs":         LanguageOptions("en-US"),
		"title":         "页面管理",
		"captcha_image": "data:image/png;base64,AAA",
	}

	// 1) 兜底：i18n 未初始化 → t 返回模板内中文原文
	fallbackData := map[string]any{}
	for k, v := range base {
		fallbackData[k] = v
	}
	fallbackData["t"] = TranslateFunc("en-US")

	loginOut, err := render(t, set, "admin/login", fallbackData)
	if err != nil {
		t.Fatalf("登录页渲染失败: %v", err)
	}
	// 当前语言（en-US）为不可点的高亮项，另一语言渲染为 ?lang= 链接（登录前切换方式）。
	for _, want := range []string{"用户名", "密码", "登 录", "href=\"/admin/login?lang=zh-CN\""} {
		if !strings.Contains(loginOut, want) {
			t.Fatalf("登录页兜底渲染缺少 %q", want)
		}
	}

	layoutOut, err := render(t, set, "admin/layout", fallbackData)
	if err != nil {
		t.Fatalf("布局渲染失败: %v", err)
	}
	for _, want := range []string{"切换主题", "关闭", "管理后台", "lang=\"en-US\""} {
		if !strings.Contains(layoutOut, want) {
			t.Fatalf("布局兜底渲染缺少 %q", want)
		}
	}

	// 2) 切换：注入英文翻译函数 → 文案变英文（等价于 lang=en-US 命中 en-US 词条）
	enData := map[string]any{}
	for k, v := range base {
		enData[k] = v
	}
	en := map[string]string{
		"shell.brand":                 "Admin Console",
		"shell.action.toggle_theme":   "Toggle theme",
		"shell.action.close":          "Close",
		"shell.login.title":           "Sign in",
		"shell.login.username":        "Username",
		"shell.login.password":        "Password",
		"shell.login.captcha":         "Captcha",
		"shell.login.submit":          "Sign in",
		"shell.login.captcha_refresh": "Click to refresh",
	}
	enData["t"] = func(key, fallback string) string {
		if v, ok := en[key]; ok {
			return v
		}
		return fallback
	}

	loginEN, err := render(t, set, "admin/login", enData)
	if err != nil {
		t.Fatalf("登录页英文渲染失败: %v", err)
	}
	for _, want := range []string{"Username", "Password", "Captcha", "Admin Console", "Click to refresh"} {
		if !strings.Contains(loginEN, want) {
			t.Fatalf("登录页英文渲染缺少 %q", want)
		}
	}
	if strings.Contains(loginEN, "用户名") {
		t.Fatal("注入英文翻译后不应再出现中文原文")
	}

	layoutEN, err := render(t, set, "admin/layout", enData)
	if err != nil {
		t.Fatalf("布局英文渲染失败: %v", err)
	}
	for _, want := range []string{"Toggle theme", "Close", "Admin Console"} {
		if !strings.Contains(layoutEN, want) {
			t.Fatalf("布局英文渲染缺少 %q", want)
		}
	}
}

// testNavNode / testNavGroup 复刻 dashboard 包的导航渲染结构（字段名与模板访问一致）。
type testNavNode struct {
	Title    string
	Path     string
	Children []testNavNode
	Active   bool
	Open     bool
}

// 菜单真源是 sys_menus：一级项用表里的 id 做前后端配对（data-group），
// 图标取表里的 icon key；目录（Nodes 非空）与一级直接链接（Nodes 为空、有 Path）两种形态。
type testNavGroup struct {
	ID       uint64
	Title    string
	Icon     string
	IconSVG  string
	Path     string
	Nodes    []testNavNode
	Active   bool
	FirstURL string
}

// TestShellSidebarPartialsRender 覆盖侧栏（固定态）与二级菜单展开按钮的文案：
// 兜底中文原文 + 注入英文翻译后变英文（shell.sidebar.* / shell.nav.*）。
func TestShellSidebarPartialsRender(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))

	groups := []testNavGroup{{
		ID:       1,
		Title:    "内容",
		Icon:     "file-text",
		IconSVG:  `<svg viewBox="0 0 24 24"><path d="M5 4h9l5 5v11H5z"/></svg>`,
		Active:   true,
		FirstURL: "/admin/pages",
		Nodes: []testNavNode{{
			Title: "页面管理", Path: "/admin/pages", Active: true, Open: true,
			Children: []testNavNode{{Title: "回收站", Path: "/admin/pages?trash=1"}},
		}},
	}}
	base := map[string]any{
		"NavGroups":     groups,
		"HasSubnav":     true,
		"SidebarPinned": true,
		"lang":          "en-US",
	}

	// 兜底：i18n 未初始化 → 中文原文
	zhData := map[string]any{}
	for k, v := range base {
		zhData[k] = v
	}
	zhData["t"] = TranslateFunc("zh-CN")
	zh, err := render(t, set, "admin/partials/sidebar", zhData)
	if err != nil {
		t.Fatalf("侧栏渲染失败: %v", err)
	}
	for _, want := range []string{
		"固定后导航不再自动收起", "已固定", "导航后保持展开",
		"展开 / 收起子菜单",
		"data-label-pinned=\"已固定\"", "data-hint-unpinned=\"点击菜单后收起\"",
	} {
		if !strings.Contains(zh, want) {
			t.Fatalf("侧栏中文渲染缺少 %q", want)
		}
	}

	// 英文词条：shell.sidebar.* / shell.nav.*
	en := map[string]string{
		"shell.sidebar.pin_title":     "Keep the sidebar open after navigating",
		"shell.sidebar.pinned":        "Pinned",
		"shell.sidebar.pin":           "Pin sidebar",
		"shell.sidebar.pinned_hint":   "Stays open after navigating",
		"shell.sidebar.unpinned_hint": "Collapses after clicking a menu",
		"shell.nav.toggle_children":   "Expand / collapse submenu",
	}
	enData := map[string]any{}
	for k, v := range base {
		enData[k] = v
	}
	enData["t"] = func(key, fallback string) string {
		if v, ok := en[key]; ok {
			return v
		}
		return fallback
	}
	out, err := render(t, set, "admin/partials/sidebar", enData)
	if err != nil {
		t.Fatalf("侧栏英文渲染失败: %v", err)
	}
	for _, want := range []string{"Pinned", "Stays open after navigating", "Expand / collapse submenu"} {
		if !strings.Contains(out, want) {
			t.Fatalf("侧栏英文渲染缺少 %q", want)
		}
	}
	if strings.Contains(out, "已固定") || strings.Contains(out, "展开 / 收起子菜单") {
		t.Fatal("注入英文翻译后不应再出现中文原文")
	}
}

// TestI18nEntriesPageRenders 词条页完整渲染（审计 I18N-003）。
//
// 渲染测试在这类页面上不是形式主义：Jet 的 if 遇到缺失的键会**中断渲染但状态码仍是 200**，
// 现象是「表格后半截没了」—— 从页面上看像数据少了，实际是模板中间断掉。
// 因此这里断言的是**表格尾部**（分页提示与保存表单）确实出现在输出里，
// 并额外跑一次空分类，确认「下拉为空」不会把模板带断。
type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}

type articleTranslationRow struct {
	Context    string
	Field      string
	FieldLabel string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	Rich       bool
}

type articleTranslationGroup struct {
	Key        string
	EntityType string
	EntityID   string
	Title      string
	Subtitle   string
	Rows       []articleTranslationRow
}

func TestI18nEntriesPageRenders(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	base := map[string]any{
		"lang": "zh-CN", "langs": LanguageOptions("zh-CN"), "title": "文案词条",
		"t": TranslateFunc("zh-CN"), "csrf_token": "tok",
		"PermSet": map[string]bool{"i18n:manage": true},
		"Keyword": "", "LangFilter": "", "CatFilter": "",
		"I18nEditURLs": []string{"/admin/i18n/edit?key=site.component.gallery.prev&lang=en-US"},
		"Saved":        "site.component.gallery.prev · en-US", "Errored": "",
		"Entries": []i18n.Entry{{
			Key: "site.component.gallery.prev", Lang: "en-US", Value: "Previous",
			Category: "ui", UpdateTime: "2026-09-14 10:00",
		}},
		"Categories": []string{"ui", "shell"},
	}
	out, err := render(t, set, "admin/system/i18n", base)
	if err != nil {
		t.Fatalf("词条页渲染失败: %v", err)
	}
	// 断言覆盖新版式的三处要点：领域说明进 .help（不再铺在首屏）、筛选并入列表卡、
	// 行内编辑按需 GET、删除与新建抽屉模板都在（原版这三块分散在三张卡里）。
	for _, want := range []string{
		"文案词条", "page-head", "help-pop",
		"filter-bar", "table-scroll", "data-table",
		"site.component.gallery.prev", "Previous",
		"/admin/i18n/save", "/admin/i18n/delete", "csrf_token",
		`data-drawer-open="#tpl-i18n-create"`, "tpl-i18n-create",
		`data-drawer-url="/admin/i18n/edit?key=site.component.gallery.prev`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("词条页缺少 %q（模板可能中途中断）", want)
		}
	}

	// 空分类 / 空列表：列表区退化为空状态，但新建抽屉模板必须仍在 ——
	// 否则运营连第一条词条都建不出来。
	empty := map[string]any{}
	for k, v := range base {
		empty[k] = v
	}
	empty["Entries"] = []i18n.Entry{}
	empty["Categories"] = []string{}
	empty["Saved"] = ""
	out, err = render(t, set, "admin/system/i18n", empty)
	if err != nil {
		t.Fatalf("空列表渲染失败: %v", err)
	}
	for _, want := range []string{"empty-state", "tpl-i18n-create"} {
		if !strings.Contains(out, want) {
			t.Fatalf("空列表缺少 %q", want)
		}
	}

	// 无 i18n:manage 权限：管理入口（编辑 / 新建）必须消失，但列表与筛选照常 ——
	// 权限缺失要 fail closed，页面上不该留下点了会被拒的按钮。
	readonly := map[string]any{}
	for k, v := range base {
		readonly[k] = v
	}
	readonly["PermSet"] = map[string]bool{}
	out, err = render(t, set, "admin/system/i18n", readonly)
	if err != nil {
		t.Fatalf("无管理权限渲染失败: %v", err)
	}
	if strings.Contains(out, "data-drawer-open") {
		t.Fatalf("无 i18n:manage 权限时不应出现编辑 / 新建抽屉入口")
	}
	if !strings.Contains(out, "filter-bar") || !strings.Contains(out, "site.component.gallery.prev") {
		t.Fatalf("无管理权限时列表与筛选仍应渲染")
	}
}

// TestArticleTranslationsPageRenders 文章翻译工作台完整渲染（审计 I18N-006）。
//
// 断言的是**表单尾部**（保存按钮）与三组同序字段（rowContext / rowHash / rowTarget）：
// 保存端用 PostFormArray 按位置对齐三组值，模板少输出一个隐藏域就会整体错位 ——
// 而错位的表现是「译文写到了别的字段上」，页面看起来完全正常。
type navigationTranslationRow struct {
	Context    string
	NodeID     string
	Kind       string
	Path       string
	Source     string
	SourceHash string
	Target     string
	Translated bool
}

type navigationTranslationGroup struct {
	Kind  string
	Title string
	Rows  []navigationTranslationRow
}

func TestArticleTranslationsPageRenders(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	data := map[string]any{
		"lang": "en-US", "Langs": []translationLangOption{{Code: "en-US", Label: "en-US", Active: true}},
		"title": "文章翻译", "menu": "article-translations", "t": TranslateFunc("en-US"),
		"csrf_token": "tok", "Lang": "en-US", "Saved": true, "SavedNote": "已保存 1 条译文（下次构建生效）。",
		"Errors": []string{}, "RowCount": 2, "Done": 1, "Total": 1,
		"Groups": []articleTranslationGroup{{
			Key: "a-1", EntityType: "article", EntityID: "a-1", Title: "标题", Subtitle: "slug",
			Rows: []articleTranslationRow{
				{Context: "article.title", Field: "title", FieldLabel: "标题", Source: "中文标题", SourceHash: "h1", Target: "English", Translated: true},
				{Context: "article.body", Field: "body", FieldLabel: "正文", Source: "<p>正文</p>", SourceHash: "h2", Rich: true},
			},
		}},
	}
	out, err := render(t, set, "admin/content/article_translations", data)
	if err != nil {
		t.Fatalf("文章翻译页渲染失败: %v", err)
	}
	for _, want := range []string{"保存译文", "rowContext", "rowHash", "rowTarget", "<textarea", "已保存 1 条译文"} {
		if !strings.Contains(out, want) {
			t.Fatalf("文章翻译页缺少 %q（模板可能中途中断或语法写错）", want)
		}
	}
	if strings.Contains(out, "{{/") {
		t.Fatal("输出里残留模板语法字面量（Jet 的 if 结束是 end 而不是 /if）")
	}
}

// TestNavigationTranslationsPageRenders 导航译文工作台完整渲染（审计 I18N-007）。
//
// 与文章工作台同一类断言：保存端用 PostFormArray 按位置对齐 rowContext / rowHash / rowTarget，
// 模板少输出一个隐藏域就会整体错位 —— 而错位的表现是「译文写到了别的菜单项上」，
// 页面看起来完全正常。另外断言空位置分支（没有菜单项时不该整页消失）。
func TestNavigationTranslationsPageRenders(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	data := map[string]any{
		"title": "导航译文", "menu": "navigation-translations", "t": TranslateFunc("en-US"),
		"csrf_token": "tok", "Lang": "en-US", "ProjectID": "p-1",
		"Langs": []translationLangOption{{Code: "en-US", Label: "en-US", Active: true}},
		"Saved": true, "SavedNote": "已保存 1 条译文；已标记受影响页面待重建（下次构建生效）。",
		"Errors": []string{}, "RowCount": 1, "Done": 0,
		"Groups": []navigationTranslationGroup{
			{Kind: "header", Title: "页眉导航", Rows: []navigationTranslationRow{
				{Context: "navigation.label", NodeID: "n-1", Kind: "header", Path: "/shop",
					Source: "商品", SourceHash: "h1"},
			}},
			{Kind: "footer", Title: "页脚导航", Rows: []navigationTranslationRow{}},
		},
	}
	out, err := render(t, set, "admin/navigation/navigation_translations", data)
	if err != nil {
		t.Fatalf("导航译文页渲染失败: %v", err)
	}
	for _, want := range []string{
		"保存译文", "rowContext", "rowHash", "rowTarget",
		"商品", "/shop", "该位置还没有菜单项",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("导航译文页缺少 %q（模板可能中途中断）", want)
		}
	}
	if strings.Contains(out, "{{/") {
		t.Fatal("输出里残留模板语法字面量")
	}
}
