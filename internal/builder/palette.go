package builder

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"sort"

	"go_wp/internal/builder/core"
)

var PaletteExemptTypes = map[string]string{
	"core.globalref":  "全局块引用：由画布的块列表拖入创建（canvas.js 带 blockId 构造），组件库条目给不出块 ID",
	"core.layoutSlot": "结构槽位：编译期由主题的页眉/页脚绑定展开成 root 首尾节点（WithStructureSlots），作者不应手动插入",
}

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
		if meta.NeedsEntityContext {
			component, err := core.Lookup(name)
			if err != nil {
				return nil, err
			}
			provider, ok := component.(core.FieldBindingProvider)
			if !ok {
				return nil, fmt.Errorf("组件 %s 声明实体上下文但未提供字段绑定", name)
			}
			raw, err := json.Marshal(meta.DefaultProps)
			if err != nil {
				return nil, err
			}
			refs, err := provider.FieldBindings(&core.Node{Type: name, Props: raw})
			if err != nil {
				return nil, fmt.Errorf("组件 %s 默认字段绑定无效: %w", name, err)
			}
			seen := map[string]bool{}
			for _, ref := range refs {
				if ref.EntityType != "" && !seen[ref.EntityType] {
					seen[ref.EntityType] = true
					meta.RequiredEntityTypes = append(meta.RequiredEntityTypes, ref.EntityType)
				}
			}
			if len(meta.RequiredEntityTypes) == 0 {
				return nil, fmt.Errorf("组件 %s 声明实体上下文但默认字段绑定为空", name)
			}
			sort.Strings(meta.RequiredEntityTypes)
		}
		out[name] = meta
	}
	return out, nil
}

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
