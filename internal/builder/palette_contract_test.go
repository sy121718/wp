package builder

// palette_contract_test.go — 组件库清单的硬边界断言（审计 REG-005）。
//
// 三条不变量，全部不依赖 Node（普通 go test 就能拦住漂移）：
//
//  1. 组件库输出的组件集合与 core.Types() 完全一致（显式豁免表除外）——
//     新增组件只改 Go，前端不需要动；漏登记组件即报错并指出差集。
//  2. 组件库条目的默认 Props 直接构造节点必须通过校验（「插入即不合法」是缺陷）。
//     结构型组件（需要默认子树的）由 defaults_contract_test.go 的 node 探针覆盖，
//     本测试要求那份清单与组件库输出保持同步（清单过期即报错）。
//  3. 覆盖检查本身具备失败能力：注入「只注册不上组件库」的反例必须被判定为缺口
//     —— 恒真的断言等于没有断言（见 TestPaletteCoverageGapsDetectsMissing）。

import (
	"encoding/json"
	"sort"
	"testing"

	"go_wp/internal/builder/core"
)

// paletteNeedsChildren 需要默认子树的组件：它们的「插入即合法」不在本测试覆盖范围内。
//
// 为什么在测试里显式列出而不是自动判断：默认子树是**节点树**（不是 Props），
// 声明在前端 DEFAULT_CONTENT（见 palette.js 文件头），Go 侧拿不到；这里只钉住
// 「这份清单没有过期」—— 组件删掉/改名后清单必须跟着收，否则本测试报错。
var paletteNeedsChildren = map[string]string{
	"core.tabs":      "页签：Validate 要求至少一个面板（默认子树在 palette.js 的 DEFAULT_CONTENT）",
	"core.accordion": "手风琴：Validate 要求至少一个折叠项",
	"core.slider":    "轮播：Validate 要求至少一个 slide",
	"core.marquee":   "跑马灯：Validate 要求至少一个内容",
	"core.cardstack": "卡片堆叠：可选子节点，但默认插入带两张内容卡（见 DEFAULT_CONTENT）",
}

// TestPaletteCoverageMatchesRegistry 组件库输出必须覆盖全部注册组件。
//
// 边界与 REG-002 的组件数量断言同源：注册表是唯一权威，组件库输出必须与它对齐，
// 差集直接报出「缺哪个 / 多哪个」。
func TestPaletteCoverageMatchesRegistry(t *testing.T) {
	items, err := ComponentPalette()
	if err != nil {
		t.Fatalf("采集组件库元数据失败: %v", err)
	}
	types := core.Types()
	if len(types) == 0 {
		t.Fatal("组件注册表为空，组件包未初始化")
	}
	missing, staleExempt, orphan := PaletteCoverageGaps(types, items, PaletteExemptTypes)
	if report := PaletteGapReport(missing, staleExempt, orphan); report != "" {
		t.Fatalf("%s（注册组件 %d 个 / 组件库 %d 项 / 豁免 %d 个）", report, len(types), len(items), len(PaletteExemptTypes))
	}
	// 数量硬账：注册组件 = 组件库输出 + 豁免（一一对应，没有第三种状态）。
	if got, want := len(items)+len(PaletteExemptTypes), len(types); got != want {
		t.Fatalf("组件库输出与注册表数量不符：组件库 %d + 豁免 %d = %d，注册表 %d", len(items), len(PaletteExemptTypes), got, want)
	}
	t.Logf("组件库覆盖一致：注册 %d = 组件库 %d + 豁免 %d", len(types), len(items), len(PaletteExemptTypes))
}

// TestPaletteDefaultPropsInsertable 组件库每个条目的默认 Props 必须直接可用。
//
// 「库存里有某个组件，但拖进去就编译失败」是最难排查的一类缺陷（症状在别的页面出现）。
// 这里在 Go 侧直接校验：默认 Props 构造的节点必须通过 ValidateNode。
func TestPaletteDefaultPropsInsertable(t *testing.T) {
	items, err := ComponentPalette()
	if err != nil {
		t.Fatalf("采集组件库元数据失败: %v", err)
	}
	for _, typ := range sortedPaletteTypes(items) {
		if _, needsChildren := paletteNeedsChildren[typ]; needsChildren {
			continue
		}
		meta := items[typ]
		raw, err := json.Marshal(meta.DefaultProps)
		if err != nil {
			t.Errorf("组件 %s 的默认 Props 序列化失败: %v", typ, err)
			continue
		}
		node := &core.Node{ID: "probe-1", Type: typ, Props: raw}
		if err = core.ValidateNode(node, map[string]bool{}); err != nil {
			t.Errorf("组件 %s 的默认 Props 构造的节点校验失败（拖进组件库即编译失败）: %v", typ, err)
		}
	}
	// 结构型清单不能变成僵尸：清单里的类型必须真的在组件库里。
	for typ := range paletteNeedsChildren {
		if _, ok := items[typ]; !ok {
			t.Errorf("跳过清单里的 %s 已不在组件库输出中（清单过期，请删除）", typ)
		}
	}
}

// TestPaletteCoverageGapsDetectsMissing 反例验证：覆盖检查必须能真的失败。
//
// 恒真的断言等于没有断言，所以这里注入三种畸形输入，逐条确认检查会报出来：
//   - 只注册、不上组件库的组件（本批要钉住的那一类）
//   - 真实组件从组件库输出里被拿掉（模拟手工删条目 / 生成器漏采集）
//   - 组件库条目指向未注册的类型（模拟组件被删但条目还在）
func TestPaletteCoverageGapsDetectsMissing(t *testing.T) {
	items, err := ComponentPalette()
	if err != nil {
		t.Fatalf("采集组件库元数据失败: %v", err)
	}
	types := core.Types()

	// 反例一：注册表里多一个既无组件库元数据、也不在豁免表的类型。
	withProbe := append(append([]string{}, types...), "probe.unpalette")
	missing, _, _ := PaletteCoverageGaps(withProbe, items, PaletteExemptTypes)
	if !containsString(missing, "probe.unpalette") {
		t.Fatalf("反例未被检出：只注册不上组件库的类型应当报缺（实际 missing=%v）", missing)
	}

	// 反例二：把一个真实组件从组件库输出里拿掉。
	victim := "core.heading"
	if _, ok := items[victim]; !ok {
		t.Fatalf("反例基准不成立：%s 不在组件库输出中", victim)
	}
	pruned := map[string]core.PaletteMeta{}
	for k, v := range items {
		if k != victim {
			pruned[k] = v
		}
	}
	missing, _, _ = PaletteCoverageGaps(types, pruned, PaletteExemptTypes)
	if !containsString(missing, victim) {
		t.Fatalf("反例未被检出：组件库输出缺少 %s 时应当报缺（实际 missing=%v）", victim, missing)
	}
	if report := PaletteGapReport(missing, nil, nil); report == "" {
		t.Fatal("缺口报告为空：PaletteGapReport 没有把缺口转成可读信息")
	}

	// 反例三：组件库条目指向未注册类型（孤儿条目）。
	orphaned := map[string]core.PaletteMeta{}
	for k, v := range items {
		orphaned[k] = v
	}
	orphaned["probe.ghost"] = core.PaletteMeta{Type: "probe.ghost", DisplayName: "幽灵", Category: core.PaletteCategoryBasic}
	_, _, orphan := PaletteCoverageGaps(types, orphaned, PaletteExemptTypes)
	if !containsString(orphan, "probe.ghost") {
		t.Fatalf("反例未被检出：组件库条目指向未注册类型时应当报孤儿（实际 orphan=%v）", orphan)
	}
}

// sortedPaletteTypes 组件库类型清单（字典序，报错信息可复现）。
func sortedPaletteTypes(items map[string]core.PaletteMeta) []string {
	out := make([]string, 0, len(items))
	for k := range items {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
