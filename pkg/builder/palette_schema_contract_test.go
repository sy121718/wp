package builder

import (
	"sort"
	"testing"
)

func TestPaletteItemsMatchSchemaByType(t *testing.T) {
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

	missingInPalette := []string{}
	for typ := range schemas {
		if _, exempt := PaletteExemptTypes[typ]; exempt {
			continue
		}
		if !inPalette[typ] {
			missingInPalette = append(missingInPalette, typ)
		}
	}

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
