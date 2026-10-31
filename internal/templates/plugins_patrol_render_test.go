package templates

import (
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"
)

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
		"Buttons": map[string]any{},
		"Plugins": []map[string]any{
			{"ID": "hello", "Name": "示例插件", "Version": "1.0.0", "ComponentCount": 2,
				"Enabled": true, "InstalledAt": "2026-01-01 00:00:00"},
		},
		"Error":          "",
		"ArtifactPatrol": patrol,
	}
}

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

	if got := patrolFoldCount(out); got != 1 {
		t.Fatalf("四类巡检应收进 1 张折叠卡（details.section-fold.card），实际 %d 张", got)
	}
	card := patrolFoldCard(out)
	summary := patrolSummary(out)
	if summary == "" {
		t.Fatalf("折叠卡缺少 <summary> —— 不一致计数就没有常显的位置")
	}

	if !strings.Contains(summary, "badge badge-warning") {
		t.Fatalf("summary 的不一致计数必须用告警色 badge-warning，实际 %q", summary)
	}

	if !strings.Contains(summary, "4 类不一致") {
		t.Fatalf("summary 应显示不一致类数（4 类不一致），实际 %q", summary)
	}
	if strings.Contains(summary, "5") {
		t.Fatalf("summary 的计数是类数口径，不能变成条目总数，实际 %q", summary)
	}

	if !strings.Contains(card, "table-wrap table-scroll") {
		t.Fatalf("折叠区内的孤儿 schema 表脱离了 table-scroll（窄屏无法横向滚动）")
	}

	if strings.Contains(card, installFormAnchor) || strings.Contains(card, `id="plugin-install"`) {
		t.Fatalf("安装表单被折进了巡检折叠区 —— 它必须留在折叠区之外")
	}
	if idx, cardIdx := strings.Index(out, installFormAnchor), strings.Index(out, `id="plugin-install"`); cardIdx < 0 || idx < cardIdx {
		t.Fatalf("安装表单应排在巡检折叠区之后（页尾），实际 fold=%d install=%d", cardIdx, idx)
	}
}

func TestPluginsPagePatrolEmptyRendersWholePage(t *testing.T) {

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

	if got := patrolFoldCount(out); got != 0 {
		t.Fatalf("无不一致时不该出现巡检折叠卡，实际 %d 张", got)
	}
}

func TestPluginsPageRendersWithoutPatrolKey(t *testing.T) {

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

func patrolFoldCount(out string) int {
	return strings.Count(out, `class="section-fold card"`)
}

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

func patrolSummary(out string) string {
	card := patrolFoldCard(out)
	i := strings.Index(card, "<summary>")
	j := strings.Index(card, "</summary>")
	if i < 0 || j < i {
		return ""
	}
	return card[i+len("<summary>") : j]
}
