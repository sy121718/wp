package builder

// palette.go — 组件库元数据的采集与覆盖检查（审计 REG-005）。
//
// 单一真源：组件的组件库呈现元数据声明在组件侧（core.AtomSpec 的
// DisplayName / Hint / PaletteCategory / DefaultProps，或自定义组件的
// Palette() 方法）。本文件只做两件事：
//
//  1. 采集（ComponentPalette）—— 给工作台生成文件和断言用；
//  2. 覆盖检查（PaletteCoverageGaps）—— 钉住「palette 输出的组件集合
//     与 core.Types() 完全一致（豁免表除外）」，且检查逻辑是纯函数，
//     可以用注入反例验证它真的会失败（见 palette_contract_test.go）。

import (
	"encoding/json"
	"fmt"
	"sort"

	"go_wp/internal/builder/core"
)

// PaletteExemptTypes 合法不进组件库的组件及理由。
//
// 豁免不是「忽略漂移」，而是把「这些组件为什么不上面板」从隐性事实变成显式声明：
// 它们都能编译、都有 schema，只是**由专门路径产生**，静态的组件库条目表达不了
// 它们需要的上下文（块 ID / 主题绑定），拖进去反而会生成非法节点。
var PaletteExemptTypes = map[string]string{
	"core.globalref":  "全局块引用：由画布的块列表拖入创建（canvas.js 带 blockId 构造），组件库条目给不出块 ID",
	"core.layoutSlot": "结构槽位：编译期由主题的页眉/页脚绑定展开成 root 首尾节点（WithStructureSlots），作者不应手动插入",
}

// ComponentPalette 采集已注册组件的组件库元数据（审计 REG-005）。
//
// 只含「声明了组件库元数据」的组件（core.PaletteOf 判定：显示名与分组齐全）。
// 每个组件的 DefaultProps 都做一次序列化校验：元数据进的是生成文件，
// 不可序列化的值必须在生成期就失败，而不是让前端拿到半截数据。
func ComponentPalette() (map[string]core.PaletteMeta, error) {
	out := make(map[string]core.PaletteMeta, len(core.Types()))
	for _, name := range core.Types() {
		meta, ok := core.PaletteOf(name)
		if !ok {
			continue
		}
		if len(meta.DefaultProps) > 0 {
			if _, err := json.Marshal(meta.DefaultProps); err != nil {
				return nil, fmt.Errorf("组件 %s 的默认 Props 无法序列化: %w", name, err)
			}
		}
		out[name] = meta
	}
	return out, nil
}

// PaletteCoverageGaps 组件库覆盖检查的纯逻辑（不读全局注册表，可注入反例）。
//
// 三条边界：
//   - missing     注册了但既没有组件库元数据、也不在豁免表里（作者拖不出来）
//   - staleExempt 豁免表里的组件已不在注册表，或已进了组件库（名单要跟着收）
//   - orphan      组件库条目指向未注册的类型（拖进去必然编译失败）
//
// 入参 types 为注册表类型清单，items 为组件库输出，exempt 为显式豁免表。
func PaletteCoverageGaps(types []string, items map[string]core.PaletteMeta, exempt map[string]string) (missing, staleExempt, orphan []string) {
	registered := make(map[string]bool, len(types))
	for _, t := range types {
		registered[t] = true
	}
	for _, t := range types {
		if _, inPalette := items[t]; inPalette {
			continue
		}
		if _, isExempt := exempt[t]; isExempt {
			continue
		}
		missing = append(missing, t)
	}
	for t := range exempt {
		// 过期豁免有两种形态：组件已不存在（删除/改名），或组件已经进了组件库
		// （豁免表没跟着收 —— 两处声明互相矛盾，必须收口到一处）。
		_, inItems := items[t]
		if !registered[t] || inItems {
			staleExempt = append(staleExempt, t)
		}
	}
	for t := range items {
		if !registered[t] {
			orphan = append(orphan, t)
		}
	}
	sort.Strings(missing)
	sort.Strings(staleExempt)
	sort.Strings(orphan)
	return missing, staleExempt, orphan
}

// PaletteGapReport 生成覆盖缺口的中文报告（测试与生成器共用措辞）。
func PaletteGapReport(missing, staleExempt, orphan []string) string {
	if len(missing) == 0 && len(staleExempt) == 0 && len(orphan) == 0 {
		return ""
	}
	report := ""
	if len(missing) > 0 {
		report += fmt.Sprintf("以下组件已注册，却既没有组件库元数据也不在豁免表里（作者拖不出来）: %v；", missing)
	}
	if len(staleExempt) > 0 {
		report += fmt.Sprintf("豁免表已过期（组件已不在注册表，或已进组件库却仍留在豁免表里）: %v；", staleExempt)
	}
	if len(orphan) > 0 {
		report += fmt.Sprintf("组件库条目指向未注册的类型（拖进去必然编译失败）: %v；", orphan)
	}
	return report
}
