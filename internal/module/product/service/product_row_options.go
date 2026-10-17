// product_row_options.go — 行装配用的纯函数里**另有页面在用**的那两个（导出）。
//
// 它们随行装配一起从 inbound/http 下沉，但不止行在用：规格文本还被捆绑构成面板与改价
// 预览用，分类下拉选项还被分类页的「父级」下拉用。留两份实现就是留两条会分叉的真源，
// 所以放在这里导出（同 CategoryLabel / FlattenCategoryTree / VariationAttributes 的既有做法）。
package productservice

import (
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"

	productdto "go_wp/internal/module/product/dto"
)

// SpecLabel 规格组合的可读文本：按属性组定义顺序把「组名 值名」拼起来。
//
// 组已被删除或 key 改过的历史组合用原始 key→值兜底显示，不让后台丢信息。
func SpecLabel(raw json.RawMessage, attrs []*productdto.AttributeResp) string {
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return "—"
	}
	used := map[string]bool{}
	parts := make([]string, 0, len(m))
	for _, a := range attrs {
		if a == nil {
			continue
		}
		value, ok := m[a.Key]
		if !ok {
			continue
		}
		used[a.Key] = true
		label := value
		for _, v := range a.Values {
			if v.Key == value {
				label = v.Label
				break
			}
		}
		parts = append(parts, a.Name+" "+label)
	}
	rest := make([]string, 0, len(m))
	for k, v := range m {
		if !used[k] {
			rest = append(rest, k+" "+v)
		}
	}
	sort.Strings(rest)
	parts = append(parts, rest...)
	return strings.Join(parts, " · ")
}

// CategoryPickOptions 分类下拉的数据项（父级下拉与商品行的主分类下拉共用）。
//
// Selected / Checked 由调用方按行的 ParentID 或商品的取值比对得出，本函数只给
// 「id + 带层级缩进的分类名」—— 层级缩进的真源只有 CategoryLabel 一处，
// 树的扁平化同理（FlattenCategoryTree）。
func CategoryPickOptions(flat []*productdto.CategoryResp) []map[string]any {
	out := make([]map[string]any, 0, len(flat))
	for _, node := range flat {
		out = append(out, map[string]any{"ID": node.ID, "Label": CategoryLabel(node)})
	}
	return out
}
