package templates

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
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
func render(t *testing.T, set *jet.Set, name string, data any) (string, error) {
	t.Helper()
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

type testNavGroup struct {
	Key      string
	Title    string
	Overview *testNavNode
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
		Key:      "content",
		Title:    "内容",
		Active:   true,
		FirstURL: "/admin/pages",
		Overview: &testNavNode{Title: "内容总览", Path: "/admin/pages", Active: true},
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
