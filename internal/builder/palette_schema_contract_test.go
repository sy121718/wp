// palette_schema_contract_test.go — 组件库清单与 Inspector schema 的一致性契约（审计 EDT-008）。
//
// 问题：组件库条目（internal/templates/static/js/workbench/palette.js 的 paletteItems）
// 与 Inspector schema（builder.ComponentSchemas()）是两份**独立维护**的数据。
// 二者一旦漂移，症状都是静默的：
//
//   - schema 有、palette 没有 —— 组件已注册且文档里能编译，但作者在面板上拖不出来；
//   - palette 有、schema 没有 —— 拖进去能用，但检查器面板为空（字段无从编辑）。
//
// 既有的 TestPaletteGroupsCoverItems 只覆盖「分组 ↔ 条目」互相覆盖，管不到这里。
//
// 为什么用一致性测试而不是代码生成：paletteItems 里真正宝贵的是 label / hint / props
// （中文标签与「插入即合法」的默认内容），这些在 schema 里**不存在**（schema 只有
// key / kind / options / default 这类控件描述符），生成不出来；schema 能生成的只有
// 「组件类型集合」这一项，而钉住集合的一致性用测试更直接 —— 无需构建步骤，
// 本地与 CI 都能在漂移进入主干前拦住。
//
// 环境没有 node 时自动跳过（与 defaults_contract_test.go 同一套探针）。
package builder

import (
	"sort"
	"testing"
)

// paletteExemptTypes 合法不进组件库的组件及理由。
//
// 豁免不是「忽略漂移」，而是把「这些组件为什么不上面板」从隐性事实变成显式声明：
// 它们都能编译、都有 schema，只是**由专门路径产生**，静态的组件库条目表达不了
// 它们需要的上下文（块 ID / 主题绑定），拖进去反而会生成非法节点。
var paletteExemptTypes = map[string]string{
	"core.globalref":  "全局块引用：由画布的块列表拖入创建（canvas.js 带 blockId 构造），组件库条目给不出块 ID",
	"core.layoutSlot": "结构槽位：编译期由主题的页眉/页脚绑定展开成 root 首尾节点（WithStructureSlots），作者不应手动插入",
}

// TestPaletteItemsMatchComponentSchemas 组件库条目与 Inspector schema 必须一一对应。
func TestPaletteItemsMatchComponentSchemas(t *testing.T) {
	schemas, err := ComponentSchemas()
	if err != nil {
		t.Fatalf("生成 Inspector schema 失败: %v", err)
	}
	if len(schemas) == 0 {
		t.Fatal("Inspector schema 为空，组件注册表可能未初始化")
	}

	p := runPaletteProbe(t)
	inPalette := map[string]bool{}
	duplicated := []string{}
	for _, typ := range p.ItemTypes {
		if inPalette[typ] {
			duplicated = append(duplicated, typ)
			continue
		}
		inPalette[typ] = true
	}
	if len(duplicated) > 0 {
		sort.Strings(duplicated)
		t.Errorf("组件库里有重复条目（同一个组件会出现在面板上两次）: %v", duplicated)
	}

	// 方向一：有 schema 的组件必须能被拖出来。
	missingInPalette := []string{}
	for typ := range schemas {
		if _, exempt := paletteExemptTypes[typ]; exempt {
			continue
		}
		if !inPalette[typ] {
			missingInPalette = append(missingInPalette, typ)
		}
	}

	// 豁免名单不能变成僵尸：被豁免的组件必须真的有 schema（组件删除 / 改名后名单要跟着收）。
	stale := []string{}
	for typ := range paletteExemptTypes {
		if _, ok := schemas[typ]; !ok {
			stale = append(stale, typ)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("豁免名单里的组件已不存在（豁免条目过期，请删除）: %v", stale)
	}
	sort.Strings(missingInPalette)
	if len(missingInPalette) > 0 {
		t.Errorf("以下组件已注册且有 Inspector schema，却不在组件库清单里（作者拖不出来）: %v", missingInPalette)
	}

	// 方向二：组件库条目必须有 schema，否则拖进去检查器是空面板。
	missingInSchema := []string{}
	for typ := range inPalette {
		if _, ok := schemas[typ]; !ok {
			missingInSchema = append(missingInSchema, typ)
		}
	}
	sort.Strings(missingInSchema)
	if len(missingInSchema) > 0 {
		t.Errorf("以下组件库条目没有 Inspector schema（拖进画布后检查器为空，字段无从编辑）: %v", missingInSchema)
	}

	t.Logf("组件库 %d 个条目 / Inspector schema %d 个组件，双向一致", len(inPalette), len(schemas))
}
