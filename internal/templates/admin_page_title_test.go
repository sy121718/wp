package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/pagination"

	"github.com/CloudyKit/jet/v6"
)

// 本文件守一件事：八个 admin 页面（六个列表 + 数据规则编辑 + 文案词条）的标题
// 在模板里取词，不是把词条 key 上屏。
//
// 背景：这些页面原先由控制器传 `Title: "adminTitle"`，layout 的
// pageTitle / topbarTitle 兜底分支直接渲染 `{{.title}}` —— 不做取词。结果是
// `<title>adminTitle — go_wp 管理后台</title>`，换语言也不会变。
// 现在标题归模板：每个页面用 `{{block pageTitle()}}` / `{{block topbarTitle()}}`
// 本地 t(key, 中文兜底)。控制器不再给 Title。
//
// 断言方式刻意分三层，缺一层就会留漏洞：
//  1. 解析：模板能加载（block 写错会在这里就炸）；
//  2. 中文渲染：`<title>` 与顶栏都是真实中文，且**不含 Msg 前缀的裸 key**；
//  3. 英文渲染：同一个页面注入 en 词库后输出英文，证明取词真的走了 t()，
//     而不是把中文硬编码在 block 里。

// 本文件是 package templates（内部测试包）；langZh / langEn 定义在同目录的
// render_scope_test.go 里，但那是 package templates_test —— 跨包不可见，
// 所以这里自带一份。两边必须同步，语言码本身就是契约的一部分。
const (
	titleLangZh = "zh-CN"
	titleLangEn = "en-US"
)

// adminTitledPage 是一个「模板路径 + 该页标题词条」的对照表。
type adminTitledPage struct {
	template string
	key      string
	zh       string
	en       string

	// hasDetail 标记这一页的模板读 .Detail 的字段（不是 map 键，是结构体字段）。
	// Jet 遇到 map 根上去点不存在的字段会直接运行期报错
	// （datarule_edit.html:14 no field Detail），所以这类页必须给真实 DTO 形状。
	hasDetail bool
}

// 一律写具名字面量：字段以后再加一个，这里也不会因为「位置参数不够」而编译失败
// —— 少写一个具名字段编译器给零值，写错名字编译器直接报错，两种都比静默错位安全。
var adminTitledPages = []adminTitledPage{
	{template: "admin/system/administrators.html", key: "adminTitle", zh: "管理员", en: "Administrators"},
	{template: "admin/system/roles.html", key: "MsgRolesTitle", zh: "角色管理", en: "Roles"},
	{template: "admin/system/menus.html", key: "MsgMenusTitle", zh: "菜单管理", en: "Menus"},
	{template: "admin/system/permissions.html", key: "permTitle", zh: "权限资源", en: "Permissions"},
	{template: "admin/system/departments.html", key: "deptTitle", zh: "部门管理", en: "Departments"},
	{template: "admin/system/datarules.html", key: "dataruleTitle", zh: "数据权限", en: "Data rules"},
	{template: "admin/system/datarule_edit.html", key: "dataruleTitle", zh: "数据权限", en: "Data rules", hasDetail: true},
	{template: "admin/system/i18n.html", key: "admin.i18n.title", zh: "文案词条", en: "Text entries"},
}

var (
	titleTagPattern    = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	topbarTitlePattern = regexp.MustCompile(`(?s)<div class="topbar-title">(.*?)</div>`)
)

// newAdminTitleSet 用真实磁盘模板搭一个 Jet 集：标题收口是模板层的事，
// 必须拿仓库里的模板文件验，不能手抄一份源码（抄的副本改了也不会红）。
func newAdminTitleSet(t *testing.T) *jet.Set {
	t.Helper()
	loader := testTemplateLoader()
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	InjectGlobalsForTest(set)
	return set
}

// adminTitleData 给模板铺最小必需数据：标题收口后控制器只给业务事实，
// 所以这里不给 title 键 —— 给了反而可能掩盖「模板没覆盖 block」的问题。
func adminTitleData(translate func(key, fallback string) string, lang string) map[string]any {
	// 键名与模板真实读取的一致（sidebar 读 .["NavGroups"]，按钮权限读 .Buttons[...]）；
	// 可选键一律给空值而不是缺失 —— 模板对根 map 的缺键会走 isset 分支，
	// 这里给齐是为了让「渲染中途中断」与「标题没取词」两种失败能区分开。
	return map[string]any{
		"lang":       lang,
		"langs":      LanguageOptions(lang),
		"csrf_token": "test-csrf",
		"t":          translate,
		"tr":         translate,
		"NavGroups":  []any{},
		"Buttons":    map[string]bool{},

		// 各页筛选栏与列表的真实键（用 grep 从模板里抽的，不是猜的）。
		"Rows":          []any{},
		"Total":         0,
		"Pagination":    pagination.Page{Total: 0, Page: 1, Limit: 20, PageParam: "page"},
		"ListQuery":     "",
		"FilterName":    "",
		"FilterEmail":   "",
		"FilterKeyword": "",
		"FilterCode":    "",
		"FilterModule":  "",
		"FilterDomain":  "",
		"SearchMode":    false,
		"Parents":       []any{},
		"Domains":       []any{},
		"PermChoices":   []any{},

		// 文案词条页的筛选与列表键：模板对这几个是**裸取值**
		// （i18n.html:24-32 的 entries/cats/catFilter/langFilter 与 :36 的 .Keyword），
		// 缺键会让整页在该行中断，不是走 isset 分支。
		"Entries":      []any{},
		"Categories":   []any{},
		"CatFilter":    "",
		"LangFilter":   "",
		"Keyword":      "",
		"I18nEditURLs": []string{},
		"LangOptions":  []any{},
	}
}

// adminTitleDataWithDetail 给「模板读 .Detail 结构体字段」的页面补真实形状。
//
// 为什么不能只塞 map：Jet 在 map 根上下点 .Detail.ID 时 .Detail 取到 nil，
// 再点 .ID 就是运行期错误（实测：datarule_edit.html:14 no field Detail）。
// 所以按 admindto 的真实结构体给，字段名与类型都与生产一致 ——
// 这份夹具一旦和 DTO 漂移，模板会立刻报错，不会静静渲染出空值。
func adminTitleDataWithDetail(translate func(key, fallback string) string, lang string) map[string]any {
	data := adminTitleData(translate, lang)
	data["Detail"] = admindto.RuleDetailResp{
		ID:       42,
		RuleName: "标题取词回归用的数据规则",
		Domain:   "sys_admin",
		Status:   1,
		Remark:   "夹具备注",
		Config: admindto.RuleConfigDTO{
			OmitFields: []string{"password"},
			ConditionGroups: []admindto.RuleConditionGroupDTO{{
				Logic: "AND",
				Conditions: []admindto.RuleConditionDTO{{
					Field: "username", Op: "LIKE", Value: "alice",
				}},
			}},
		},
	}
	// 换域下拉的候选事实：range 出来要非空，否则选中态分支测不到。
	data["Domains"] = []admindto.RuleDomainItem{{
		Domain: "sys_admin", DomainLabel: "管理员", TableName: "sys_admin",
	}}
	// 配置编辑器 include 读 .Editor.Fields；nil 会走「加载失败」分支，这里给真形状。
	data["Editor"] = admindto.RuleDomainDetail{
		Domain: "sys_admin", DomainLabel: "管理员", TableName: "sys_admin",
		Fields: []admindto.RuleFieldDef{{
			Field: "username", Label: "账号", Operators: []string{"EQ", "LIKE"},
		}},
	}
	return data
}

// titleScope 把测试自己的取词闭包装成执行期上下文：
// 标题所在的 include 片段用 {{tr := t}} 取执行期变量，数据里的 t 传不进去。
func titleScope(t func(key, fallback string) string, lang string) RenderScope {
	return RenderScope{
		Lang:           lang,
		Translate:      t,
		TranslateArgs:  func(key, fallback string, _ []string) string { return t(key, fallback) },
		TranslateNamed: func(key, fallback string, _ map[string]any) string { return t(key, fallback) },
	}
}

func TestAdminPageTitlesAreTranslatedInTemplate(t *testing.T) {
	set := newAdminTitleSet(t)

	for _, page := range adminTitledPages {
		page := page
		name := strings.TrimSuffix(strings.TrimPrefix(page.template, "admin/"), ".html")
		t.Run(name, func(t *testing.T) {
			zhT := func(key, fallback string) string {
				if key == page.key {
					return page.zh
				}
				return fallback
			}
			// 英文：同一个模板注入 en 词库，标题必须跟着变 ——
			// 否则说明中文是硬编码在 block 里，不是 t() 取出来的。
			enT := func(key, fallback string) string {
				if key == page.key {
					return page.en
				}
				return fallback
			}
			zhData := adminTitleData(zhT, titleLangZh)
			enData := adminTitleData(enT, titleLangEn)
			if page.hasDetail {
				zhData = adminTitleDataWithDetail(zhT, titleLangZh)
				enData = adminTitleDataWithDetail(enT, titleLangEn)
			}
			out, err := render(t, set, page.template, zhData, titleScope(zhT, titleLangZh))
			if err != nil {
				t.Fatalf("中文渲染失败: %v", err)
			}

			title, ok := extractTitleTag(out)
			if !ok {
				t.Fatalf("响应里没有 <title> 标签")
			}
			if !strings.Contains(title, page.zh) {
				t.Fatalf("<title> 应由模板取词得到 %q，实际 %q", page.zh, title)
			}
			// 裸 key 上屏是本文件要防的回归：说明 block 没覆盖，落到了 layout 的 .title 兜底。
			if strings.Contains(out, page.key) {
				t.Fatalf("页面输出里出现了标题词条的裸 key %q（block 未覆盖 layout 兜底）", page.key)
			}

			topbar, ok := extractTopbarTitle(out)
			if !ok {
				t.Fatalf("响应里没有 topbar-title")
			}
			if !strings.Contains(topbar, page.zh) {
				t.Fatalf("顶栏标题应由模板取词得到 %q，实际 %q", page.zh, topbar)
			}

			// 英文断言：同一个模板、换一份词库闭包，标题必须跟着变。
			outEn, err := render(t, set, page.template, enData, titleScope(enT, titleLangEn))
			if err != nil {
				t.Fatalf("英文渲染失败: %v", err)
			}
			titleEn, ok := extractTitleTag(outEn)
			if !ok {
				t.Fatalf("英文响应里没有 <title> 标签")
			}
			if !strings.Contains(titleEn, page.en) {
				t.Fatalf("英文 <title> 应为 %q，实际 %q", page.en, titleEn)
			}
			topbarEn, ok := extractTopbarTitle(outEn)
			if !ok {
				t.Fatalf("英文响应里没有 topbar-title")
			}
			if !strings.Contains(topbarEn, page.en) {
				t.Fatalf("英文顶栏标题应为 %q，实际 %q", page.en, topbarEn)
			}
			if strings.Contains(titleEn, page.zh) {
				t.Fatalf("英文渲染下 <title> 不应还含中文 %q：%q", page.zh, titleEn)
			}
		})
	}
}

// TestAdminControllersNoLongerSendTitleKey 守控制器那一侧：七个页面 DTO 删掉了
// Title 字段，标题只由模板给。这里不解析 Go 源码，而是直接确认渲染数据里
// 即使有人塞回 Msg* 键，模板也已经用 block 覆盖了它 —— 两层同时成立才算收口。
func TestAdminLayoutTitleFallbackStillOwnsUntitledPages(t *testing.T) {
	set := newAdminTitleSet(t)

	// layout 的兜底仍然服务「没有覆盖 pageTitle 的页面」：这是既有契约，别域还在用。
	// 这里只确认取词兜底分支没被一并改掉 —— 否则别域页面会突然失去品牌名。
	out, err := render(t, set, "admin/login.html", map[string]any{
		"lang":       titleLangZh,
		"csrf_token": "test-csrf",
		"t":          TranslateFunc(titleLangZh),
	})
	if err != nil {
		t.Fatalf("登录页渲染失败: %v", err)
	}
	title, ok := extractTitleTag(out)
	if !ok {
		t.Fatalf("登录页没有 <title>")
	}
	if strings.TrimSpace(title) == "" {
		t.Fatalf("登录页 <title> 为空，layout 兜底链断了")
	}
}

func extractTitleTag(body string) (string, bool) {
	m := titleTagPattern.FindStringSubmatch(body)
	if len(m) != 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

func extractTopbarTitle(body string) (string, bool) {
	m := topbarTitlePattern.FindStringSubmatch(body)
	if len(m) != 2 {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// TestAdminPageTitleBlocksComeAfterImports 守一个 Jet 语法约束：
//
//	{{import}} 只能出现在模板开头，出现在别的语句之后会 parse 失败
//	（jet v6.3.3 parse_test.go:90 的 late_import 用例："'import' statements must be
//	be at the beginning of the template"）。真实后果不是测试红，而是**生产页面 200 空
//	body** —— 模板执行失败但中间件缺位时状态码照样 200。
//
// 所以 extends / import 必须全部在 pageTitle / topbarTitle 之前。这条不是风格问题：
// 插错位置会让整页渲染不出来，而单看模板 diff 完全看不出来。
func TestAdminPageTitleBlocksComeAfterImports(t *testing.T) {
	set := newAdminTitleSet(t)

	for _, page := range adminTitledPages {
		page := page
		name := strings.TrimSuffix(strings.TrimPrefix(page.template, "admin/"), ".html")
		t.Run(name, func(t *testing.T) {
			// 解析本身就是最强的顺序断言：顺序错了这里就报 parse 错。
			if _, err := set.GetTemplate(page.template); err != nil {
				t.Fatalf("模板解析失败（extends/import 必须都在 title block 之前）：%v", err)
			}

			// 再按原文核对一遍顺序，让失败信息直接指出版本行号，而不是让人去读 parser 错误。
			src, err := readTemplateSource(page.template)
			if err != nil {
				t.Fatalf("读取模板源码失败：%v", err)
			}
			lastImport := -1
			for _, marker := range []string{"{{extends ", "{{import "} {
				if i := strings.LastIndex(src, marker); i > lastImport {
					lastImport = i
				}
			}
			firstTitle := strings.Index(src, "{{block pageTitle()}}")
			if firstTitle < 0 {
				t.Fatalf("模板没有 pageTitle block")
			}
			if lastImport > firstTitle {
				t.Fatalf("extends/import 出现在 pageTitle 之后，Jet 会 parse 失败（生产表现为 200 空 body）")
			}

			// 两个 block 都要有：只修 <title> 会留顶栏裸 key。
			if !strings.Contains(src, "{{block topbarTitle()}}") {
				t.Fatalf("模板缺少 topbarTitle block（顶栏会退回 layout 的 .title 兜底）")
			}
		})
	}
}

// readTemplateSource 读模板原文。标题顺序这类约束必须看源文件，
// 渲染后的 HTML 看不出 block 摆在哪个 import 前面。
func readTemplateSource(name string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(".", name))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
