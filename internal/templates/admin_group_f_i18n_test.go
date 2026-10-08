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
	"admin/system/administrators.html",
	"admin/system/roles.html",
	// 角色权限分配页（本轮新增，角色分权的落点）。纳入本批门禁：它的取词同样必须
	// 中英成对 —— 新增页面最容易漏的就是这一条（页面上的英文界面会整块回落中文）。
	"admin/system/role_permissions.html",
	"admin/system/permissions.html",
	// 权限编辑表单已从列表逐行模板移到按需片段：本批 seed 的保存文案仍在片段中使用。
	"admin/system/permission_edit_form.html",
	"admin/system/departments.html",
	"admin/system/i18n.html",
	"admin/masterdata/masterdata_changes.html",
	"admin/system/datarules.html",
	"admin/system/datarule_edit.html",
	// 数据规则配置编辑器片段：随编辑页下线裸 JSON textarea 后新增，纳入本批门禁。
	"admin/system/datarule_config_editor.html",
	"admin/analytics/analytics.html",
}

// groupFRetiredKeys 随版式改版退役的词条。
//
// 它们由 193（历史迁移，按约定保持原样）seed，但取用它们的版式已被取代：词条留在库里
// 只占一行、不影响任何页面，只是不再有任何模板取用。显式登记，而不是回头去删 193 里的行 ——
// 改历史迁移既违反约定，也不会让已跑过的库丢掉这些词条，只会让「新库少几行、老库多几行」
// 这种差异变得不可见。
var groupFRetiredKeys = map[string]bool{
	// 角色权限分配从「独立页面」改成「角色列表行的抽屉片段」后退役：「返回角色列表」
	// 这个动作由抽屉自身的关闭（✕ / 取消 / Esc，词条 shell.action.close 与
	// admin.common.action.cancel）承担 —— 抽屉本来就浮在列表页之上，不需要再给一条回列表的链接。
	// 词条由 228 seed（历史迁移保持原样），留在库里只占一行、不被任何模板取用。
	"admin.roles.perm.back": true,
	// admin/i18n.html 从「三张卡」改为「页头 + 列表卡」：分页文字说明改由 shell 的分页条
	// 承担（shell.pagination.info，见 internal/shell/pagination.go），页尾那张
	// 「新增 / 编辑」卡片由页头的新建按钮取代。
	"admin.i18n.pager.total_pre":  true,
	"admin.i18n.pager.total_post": true,
	"admin.i18n.pager.page_post":  true,
	"admin.i18n.form.title":       true,
	// 词条页的写动作结论从「查询参数回带 + 页面提示条」改为「整页提示」（shell.RenderJump）：
	// 模板里的「已保存：/ 未保存：」提示条整批删除。
	//   · admin.i18n.saved 仍被取用，只是改由 Go 侧（admin_jump.go 的 adminI18nSavedText）取词；
	//   · admin.i18n.unsaved 不再有任何取用点（失败原因由提示页承载）。
	"admin.i18n.saved":   true,
	"admin.i18n.unsaved": true,
	// admin/masterdata_changes.html：「记录表 / 实体汇总表」改为按视图二选一，
	// 两个表头里的「（共 N 条 / 个）」计数随之退役 —— 记录视图的总数改由分页条给出，
	// 实体视图只在被截断时才提示，标题不再拼计数（标题拼计数会让表头长度随数据变化）。
	"admin.masterdata.rows.headingClose":     true,
	"admin.masterdata.entities.headingClose": true,
	// 两张卡各自的标题（「筛选（按实体查变更历史）」「当前实体」）随卡片合并退役：
	// 筛选栏不再有独立标题，「当前实体」降级为页头下方的一行上下文（.page-sub）。
	"admin.masterdata.filter.title":  true,
	"admin.masterdata.current.title": true,
	// 两张表的标题（「字段级变更（共 N 条）」「按实体汇总（共 N 个实体）」）改成 .tabs 的
	// 标签后退役：同一份数据的两种看法由标签切换承担，标题不再重复它们的名字。
	"admin.masterdata.rows.heading":     true,
	"admin.masterdata.entities.heading": true,
	// admin/dashboard.html 从「组件演示页」改为「真实概览」：原页面的统计卡、假管理员表格、
	// 8 字段示例表单、徽章与按钮展台全部删除，取用它们的 64 个词条随之退役。
	// 留在库里不影响任何页面（只是不再被取用）；改 193 既违反「历史迁移保持原样」，
	// 也不会让已跑过的库少掉这些行。
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
	// 「站点内容」与「最近更新的页面」两块随 2026-10 的概览页改版下线：
	// 这一页的主内容改成 AI 提问框 + 三卡 + 趋势/榜单，站点内容的四个数字
	// （工程 / 页面总数 / 已发布 / 草稿）与最近更新的表格不再出现在概览页 ——
	// 它们各自的详情页（/admin/pages、工程列表）才是更合适的落点。
	"admin.dashboard.stat.published": true,
	"admin.dashboard.col.status":     true,
	"admin.dashboard.col.actions":    true,
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
		if groupFRetiredKeys[key] {
			continue
		}
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
		// 概览数据由 Dashboard handler 注入（真实查询，不再有静态演示值）。
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
	// base 里没有 Overview 键，走的是「只渲染外壳」那条路径：页头 + AI 提问区。
	// 概览数据块的渲染判据在 admin_dashboard_overview_test.go（那里注入 Overview）。
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
	for _, want := range []string{"Dashboard", "Ask AI (business data)"} {
		if !strings.Contains(out, want) {
			t.Errorf("仪表盘英文渲染缺少 %q", want)
		}
	}
	// 只检查被上方 en map 覆盖的 key 对应的中文。未覆盖的 key 按设计回落中文兜底
	// （「英文界面回落中文」本身是漏 seed 的表现，由 TestGroupFI18nTemplateKeysMatchSeed 守门）；
	// 也不能去扫「站点工程」这类词 —— 它同样出现在未覆盖的 intro 说明里。
	if strings.Contains(out, "仪表盘") {
		t.Error("注入英文翻译后不应再出现中文标题")
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
	admins["Buttons"] = map[string]any{"admin.create": true, "admin.edit": true, "admin.delete": true}
	admins["Rows"] = []adminRow{
		{ID: 1, Username: "admin", Name: "甲", Email: "a@example.com", Phone: "138", Status: 1},
		{ID: 2, Username: "editor01", Status: 3},
	}
	out, err := render(t, set, "admin/system/administrators", admins)
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
	out, err = render(t, set, "admin/system/i18n", i18nData)
	if err != nil {
		t.Fatalf("词条页渲染失败: %v", err)
	}
	for _, want := range []string{
		"文案词条", "site.component.gallery.prev", "编辑", "删除",
		`data-drawer-url="/admin/i18n/edit?key=site.component.gallery.prev`, "tpl-i18n-create", "来源或修改原因",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("词条页缺少 %q（range 可能在 tr 取词处中断）", want)
		}
	}
}
