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
	out, err := render(t, pluginsPatrolSet(t), "admin/plugins", pluginsPatrolData(patrolView{
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
}

func TestPluginsPagePatrolEmptyRendersWholePage(t *testing.T) {
	// 空报告（handler 在巡检失败 / 无不一致时给的形状）必须渲染完整整页，
	// 且**不出现**巡检标题 —— 没有不一致就不该占版面。
	out, err := render(t, pluginsPatrolSet(t), "admin/plugins", pluginsPatrolData(patrolView{
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
}

func TestPluginsPageRendersWithoutPatrolKey(t *testing.T) {
	// 旧调用点 / 单测外壳可能不给 SchemaPatrol 键。模板用 isset 兜住，
	// 缺键只能是「不渲染巡检区」，绝不能变成整页中断。
	data := pluginsPatrolData(patrolView{OrphanSchemas: []patrolSchemaRow{}, MissingSchemas: []string{}, OrphanStorage: []string{}, MissingStorage: []string{}})
	delete(data, "ArtifactPatrol")
	out, err := render(t, pluginsPatrolSet(t), "admin/plugins", data)
	if err != nil {
		t.Fatalf("缺 ArtifactPatrol 键时渲染失败: %v", err)
	}
	if !strings.Contains(out, installFormAnchor) {
		t.Fatalf("缺 ArtifactPatrol 键时页尾安装表单缺失 —— isset 保护没生效")
	}
}
