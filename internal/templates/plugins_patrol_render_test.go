package templates

// plugins_patrol_render_test.go — 插件管理页「数据库结构巡检」区块的渲染回归。
//
// 为什么值得单独一份：巡检区块在页面的**中后段**，而 Jet 的 if 遇到缺失或类型不符的键会
// 中断渲染、HTTP 却仍是 200 —— 现象是巡检区之后（含「安装插件」表单）整块消失，
// 而「插件列表还在」会让人以为页面正常（internal/templates/CLAUDE.md 记过这个陷阱）。
// 所以每类输入都断言**尾部标记**（页尾安装表单的 action）确实出现在输出里。
//
// 同时钉住 handler 的形状约定：巡检失败时给它的是**非 nil 空报告**（plugin_page_handle.go
// 的 emptySchemaPatrol），模板对 nil 指针取字段会中断整页 —— 这里用「空报告」作为一类输入
// 覆盖那个形状，而不是只测有数据的样子。

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

// patrolSchemaRow / patrolView 与 plugindto.SchemaPatrolResp 同形。
// 不 import 模块 dto：模板包只该认识「数据形状」，不该依赖某个业务模块的类型
// （依赖方向是模块 → 模板，反过来会让模板包被业务模块牵着走）。
type patrolSchemaRow struct {
	Name       string
	TableCount int
}

type patrolView struct {
	OrphanSchemas  []patrolSchemaRow
	MissingSchemas []string
	OrphanStorage  []string
	MissingStorage []string
	StorageRoot    string
}

func pluginsPatrolSet(t *testing.T) *jet.Set {
	t.Helper()
	return jet.NewSet(jet.NewOSFileSystemLoader("."), jet.WithTemplateNameExtensions([]string{"", ".html"}))
}

func pluginsPatrolData(patrol any) map[string]any {
	return map[string]any{
		"lang": "zh-CN", "langs": LanguageOptions("zh-CN"),
		"title": "插件管理", "menu": "plugins", "t": TranslateFunc("zh-CN"), "csrf_token": "tok",
		"PermSet": map[string]any{},
		"Plugins": []map[string]any{
			{"ID": "hello", "Name": "示例插件", "Version": "1.0.0", "ComponentCount": 2,
				"Enabled": true, "InstalledAt": "2026-01-01 00:00:00"},
		},
		"Error":          "",
		"ArtifactPatrol": patrol,
	}
}

// installFormAnchor 是页面最后一段（安装表单）的 action —— 它出现即证明整页渲染到底。
const installFormAnchor = "action=\"/admin/plugins/install\""

func TestPluginsPagePatrolReportsOrphanSchema(t *testing.T) {
	out, err := render(t, pluginsPatrolSet(t), "admin/plugin/plugins", pluginsPatrolData(patrolView{
		OrphanSchemas: []patrolSchemaRow{
			{Name: "plugin_gone", TableCount: 3},
			{Name: "plugin_empty", TableCount: 0},
		},
		MissingSchemas: []string{"broken"},
		OrphanStorage:  []string{"leftover"},
		MissingStorage: []string{"dirless"},
		StorageRoot:    "public/runtime/plugins",
	}))
	if err != nil {
		t.Fatalf("插件管理页渲染失败: %v", err)
	}
	if !strings.Contains(out, installFormAnchor) {
		t.Fatalf("页尾安装表单缺失 —— 模板可能在巡检区块处中断（HTTP 仍是 200）")
	}
	for _, want := range []string{
		// 四类不一致都要出现在页面上，且各自带可定位的数据（schema 名 / 表数量 / 插件 ID / 路径）。
		"产物对账巡检：孤儿 schema", "plugin_gone", "plugin_empty", "表数量",
		"里面有表，先确认数据是否还需要，再决定是否 DROP",
		"空 schema，确认后可交由 DBA 手工 DROP",
		"产物对账巡检：注册行缺少 schema", "broken",
		"产物对账巡检：孤儿存储目录", "public/runtime/plugins/leftover",
		"产物对账巡检：注册行缺少存储目录", "dirless",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("巡检区块缺少 %q", want)
		}
	}

	// ── 折叠形态（审计 02-L P1-17）────────────────────────────────────────────
	// 四类不一致收进**一张** details.section-fold.card：验收判据是它恰好 1 个。
	// 四个小节仍是本卡的内容分组（无 card 的 card-body），不再是四张独立卡片。
	if got := patrolFoldCount(out); got != 1 {
		t.Fatalf("四类巡检应收进 1 张折叠卡（details.section-fold.card），实际 %d 张", got)
	}
	card := patrolFoldCard(out)
	summary := patrolSummary(out)
	if summary == "" {
		t.Fatalf("折叠卡缺少 <summary> —— 不一致计数就没有常显的位置")
	}
	// 计数必须带告警色：孤儿 schema 里可能有真实数据，是安全信号，折叠不等于把它藏了。
	if !strings.Contains(summary, "badge badge-warning") {
		t.Fatalf("summary 的不一致计数必须用告警色 badge-warning，实际 %q", summary)
	}
	// 计数口径是「类数」（本用例四类都非空 → 4），不是条目总数（本用例条目共 5 条）。
	if !strings.Contains(summary, "4 类不一致") {
		t.Fatalf("summary 应显示不一致类数（4 类不一致），实际 %q", summary)
	}
	if strings.Contains(summary, "5") {
		t.Fatalf("summary 的计数是类数口径，不能变成条目总数，实际 %q", summary)
	}
	// 折叠区内的表仍在 table-scroll 里（窄屏可横向滚动）—— 合并时最容易掉的一层。
	if !strings.Contains(card, "table-wrap table-scroll") {
		t.Fatalf("折叠区内的孤儿 schema 表脱离了 table-scroll（窄屏无法横向滚动）")
	}
	// 「安装插件」在折叠区**之外**、页尾（02-S §4：安装降到页尾是有意的信息架构）。
	if strings.Contains(card, installFormAnchor) || strings.Contains(card, `id="plugin-install"`) {
		t.Fatalf("安装表单被折进了巡检折叠区 —— 它必须留在折叠区之外")
	}
	if idx, cardIdx := strings.Index(out, installFormAnchor), strings.Index(out, `id="plugin-install"`); cardIdx < 0 || idx < cardIdx {
		t.Fatalf("安装表单应排在巡检折叠区之后（页尾），实际 fold=%d install=%d", cardIdx, idx)
	}
}

func TestPluginsPagePatrolEmptyRendersWholePage(t *testing.T) {
	// 空报告（handler 在巡检失败 / 无不一致时给的形状）必须渲染完整整页，
	// 且**不出现**巡检标题 —— 没有不一致就不该占版面。
	out, err := render(t, pluginsPatrolSet(t), "admin/plugin/plugins", pluginsPatrolData(patrolView{
		OrphanSchemas: []patrolSchemaRow{}, MissingSchemas: []string{},
		OrphanStorage: []string{}, MissingStorage: []string{}, StorageRoot: "public/runtime/plugins",
	}))
	if err != nil {
		t.Fatalf("插件管理页（空巡检）渲染失败: %v", err)
	}
	if !strings.Contains(out, installFormAnchor) {
		t.Fatalf("空巡检时页尾安装表单缺失")
	}
	if !strings.Contains(out, "示例插件") {
		t.Fatalf("空巡检时插件列表仍应渲染")
	}
	for _, forbidden := range []string{"产物对账巡检：孤儿", "产物对账巡检：注册行"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("无不一致时不该渲染巡检区块 %q", forbidden)
		}
	}
	// 折叠卡本身也不该渲染：没有不一致就没有可折叠的内容（验收判据 0）。
	if got := patrolFoldCount(out); got != 0 {
		t.Fatalf("无不一致时不该出现巡检折叠卡，实际 %d 张", got)
	}
}

func TestPluginsPageRendersWithoutPatrolKey(t *testing.T) {
	// 旧调用点 / 单测外壳可能不给 SchemaPatrol 键。模板用 isset 兜住，
	// 缺键只能是「不渲染巡检区」，绝不能变成整页中断。
	data := pluginsPatrolData(patrolView{OrphanSchemas: []patrolSchemaRow{}, MissingSchemas: []string{}, OrphanStorage: []string{}, MissingStorage: []string{}})
	delete(data, "ArtifactPatrol")
	out, err := render(t, pluginsPatrolSet(t), "admin/plugin/plugins", data)
	if err != nil {
		t.Fatalf("缺 ArtifactPatrol 键时渲染失败: %v", err)
	}
	if !strings.Contains(out, installFormAnchor) {
		t.Fatalf("缺 ArtifactPatrol 键时页尾安装表单缺失 —— isset 保护没生效")
	}
	if got := patrolFoldCount(out); got != 0 {
		t.Fatalf("缺 ArtifactPatrol 键时不该出现巡检折叠卡，实际 %d 张", got)
	}
}

// TestPluginsPagePatrolFoldCountsKindsNotRows 钉住 summary 的计数口径：**类数**，不是条目数。
//
// 两类输入各自只命中一类不一致（一个里有 2 行、另一个里有 1 行），计数都必须是 1 ——
// 计数变成条目数时，summary 会随数据量变化，读者反而看不出「有几类要去处理」。
func TestPluginsPagePatrolFoldCountsKindsNotRows(t *testing.T) {
	cases := []struct {
		name   string
		patrol patrolView
	}{
		{"单类多行", patrolView{
			OrphanSchemas:  []patrolSchemaRow{{Name: "plugin_gone", TableCount: 3}, {Name: "plugin_empty", TableCount: 0}},
			MissingSchemas: []string{}, OrphanStorage: []string{}, MissingStorage: []string{},
			StorageRoot: "public/runtime/plugins",
		}},
		{"另一类单行", patrolView{
			OrphanSchemas: []patrolSchemaRow{}, MissingSchemas: []string{},
			OrphanStorage: []string{}, MissingStorage: []string{"dirless"},
			StorageRoot: "public/runtime/plugins",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := render(t, pluginsPatrolSet(t), "admin/plugin/plugins", pluginsPatrolData(tc.patrol))
			if err != nil {
				t.Fatalf("插件管理页渲染失败: %v", err)
			}
			if got := patrolFoldCount(out); got != 1 {
				t.Fatalf("有一类不一致就该有 1 张折叠卡，实际 %d 张", got)
			}
			summary := patrolSummary(out)
			if !strings.Contains(summary, "1 类不一致") {
				t.Fatalf("只有一类不一致时 summary 应显示「1 类不一致」，实际 %q", summary)
			}
			if !strings.Contains(out, installFormAnchor) {
				t.Fatalf("页尾安装表单缺失 —— 模板可能在巡检折叠卡处中断（HTTP 仍是 200）")
			}
		})
	}
}

// ── 折叠卡的结构判据 ────────────────────────────────────────────────────────
// 只看巡检折叠卡那一段（而不是整页文本）：页面上「产物对账巡检」这句话在四个小节标题里
// 都会出现，用整页 Contains 判断会把「卡里有没有某小节」与「页面上有没有这句话」混为一谈。

// patrolFoldCount 页面上 details.section-fold.card 的数量（验收判据：0 或 1）。
func patrolFoldCount(out string) int {
	return strings.Count(out, `class="section-fold card"`)
}

// patrolFoldCard 返回巡检折叠卡的整段（<details class="section-fold card"> … </details>）。
// 没有折叠卡时返回空串。巡检卡内不嵌套 details，所以第一个闭合标签就是它自己的。
func patrolFoldCard(out string) string {
	i := strings.Index(out, `<details class="section-fold card">`)
	if i < 0 {
		return ""
	}
	j := strings.Index(out[i:], "</details>")
	if j < 0 {
		return out[i:]
	}
	return out[i : i+j+len("</details>")]
}

// patrolSummary 返回巡检折叠卡 summary 的整段文本（无 summary 时为空串）。
func patrolSummary(out string) string {
	card := patrolFoldCard(out)
	i := strings.Index(card, "<summary>")
	j := strings.Index(card, "</summary>")
	if i < 0 || j < i {
		return ""
	}
	return card[i+len("<summary>") : j]
}
