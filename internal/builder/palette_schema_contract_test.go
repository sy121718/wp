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

// 组件库豁免表（PaletteExemptTypes）已上移到生产代码 palette.go：
// 覆盖断言（palette_contract_test.go）与 Inspector schema 一致性检查共用同一份声明。

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
		if _, exempt := PaletteExemptTypes[typ]; exempt {
			continue
		}
		if !inPalette[typ] {
			missingInPalette = append(missingInPalette, typ)
		}
	}

	// 豁免名单不能变成僵尸：被豁免的组件必须真的有 schema（组件删除 / 改名后名单要跟着收）。
	stale := []string{}
	for typ := range PaletteExemptTypes {
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

// TestPaletteOutputMatchesGoSpec 前端派生条目必须与 Go 侧组件库输出一一对应（审计 REG-005）。
//
// palette.js 已不再手写条目：它由生成的 paletteSpec（组件 Go 声明）派生。
// 本测试钉住派生结果不丢条目、不多条目 —— 并顺带证明「新增组件只改 Go」时，
// 前端拿到的集合等于 Go 侧注册集合。
func TestPaletteOutputMatchesGoSpec(t *testing.T) {
	items, err := ComponentPalette()
	if err != nil {
		t.Fatalf("采集组件库元数据失败: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("组件库元数据为空，组件注册表可能未初始化")
	}
	p := runPaletteProbe(t)
	jsTypes := map[string]bool{}
	for _, typ := range p.ItemTypes {
		jsTypes[typ] = true
	}

	lost := []string{}
	for typ := range items {
		if !jsTypes[typ] {
			lost = append(lost, typ)
		}
	}
	extra := []string{}
	for typ := range jsTypes {
		if _, ok := items[typ]; !ok {
			extra = append(extra, typ)
		}
	}
	sort.Strings(lost)
	sort.Strings(extra)
	if len(lost) > 0 {
		t.Errorf("以下组件有 Go 侧组件库元数据，却没出现在前端组件库（palette.js 派生丢条目）: %v", lost)
	}
	if len(extra) > 0 {
		t.Errorf("以下前端条目没有对应的 Go 侧组件库元数据（前端手写残留）: %v", extra)
	}
	t.Logf("组件库派生一致：Go 侧 %d 项 / 前端 %d 项", len(items), len(p.ItemTypes))
}
