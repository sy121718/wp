package builder

import (
	json "go_wp/pkg/jsonx"
	"reflect"
	"sort"
	"testing"

	"go_wp/internal/builder/core"
)

func TestPaletteDefaultEntityContext(t *testing.T) {
	items, err := ComponentPalette()
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"core.product", "core.productCard"} {
		if !reflect.DeepEqual(items[typ].RequiredEntityTypes, []string{"product"}) {
			t.Errorf("%s 默认插入必须声明真实商品绑定上下文: %v", typ, items[typ].RequiredEntityTypes)
		}
	}
	for _, typ := range []string{"core.productList", "core.productSelector", "core.heading"} {
		if len(items[typ].RequiredEntityTypes) != 0 {
			t.Errorf("%s 有独立配置路径，不应整体限制: %v", typ, items[typ].RequiredEntityTypes)
		}
	}
}

var paletteNeedsChildren = map[string]string{
	"core.tabs":      "页签：Validate 要求至少一个面板（默认子树在 palette.js 的 DEFAULT_CONTENT）",
	"core.accordion": "手风琴：Validate 要求至少一个折叠项",
	"core.slider":    "轮播：Validate 要求至少一个 slide",
	"core.marquee":   "跑马灯：Validate 要求至少一个内容",
	"core.cardstack": "卡片堆叠：可选子节点，但默认插入带两张内容卡（见 DEFAULT_CONTENT）",
}

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

	if got, want := len(items)+len(PaletteExemptTypes), len(types); got != want {
		t.Fatalf("组件库输出与注册表数量不符：组件库 %d + 豁免 %d = %d，注册表 %d", len(items), len(PaletteExemptTypes), got, want)
	}
	t.Logf("组件库覆盖一致：注册 %d = 组件库 %d + 豁免 %d", len(types), len(items), len(PaletteExemptTypes))
}

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

	for typ := range paletteNeedsChildren {
		if _, ok := items[typ]; !ok {
			t.Errorf("跳过清单里的 %s 已不在组件库输出中（清单过期，请删除）", typ)
		}
	}
}

func TestPaletteCoverageGapsDetectsMissing(t *testing.T) {
	items, err := ComponentPalette()
	if err != nil {
		t.Fatalf("采集组件库元数据失败: %v", err)
	}
	types := core.Types()

	withProbe := append(append([]string{}, types...), "probe.unpalette")
	missing, _, _ := PaletteCoverageGaps(withProbe, items, PaletteExemptTypes)
	if !containsString(missing, "probe.unpalette") {
		t.Fatalf("反例未被检出：只注册不上组件库的类型应当报缺（实际 missing=%v）", missing)
	}

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

func sortedPaletteTypes(items map[string]core.PaletteMeta) []string {
	out := make([]string, 0, len(items))
	for k := range items {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
