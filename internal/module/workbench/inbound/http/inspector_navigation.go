package workbenchhttp

// inspector_navigation.go — 检查器的「具体菜单项」下拉数据源（nav 组件 Props.Navigation）。
//
// nav 组件有两种引用方式：按位置（Props.Menu，静态选项）与按**具体菜单项**
//（Props.Navigation，需要列出本工程的菜单项）。后者是动态数据，走 entityref 机制：
// 组件声明 ct:"entityref,navigation"，检查器按 refKind 找这里要选项。
//
// 未注入（测试装配 / 未接导航模块）时只返回「（不限）」，控件退化为空下拉而不是报错 ——
// 与 entityref 其它来源（category/brand/tag）同一降级口径。

import (
	"context"

	navigationdto "go_wp/internal/module/navigation/dto"
)

// NavigationPickerPort 导航菜单项列表（消费者侧最窄接口）。
type NavigationPickerPort interface {
	List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error)
}

// SetNavigationPicker 注入导航菜单项列表端口（装配期调用；未注入时下拉为空）。
func (h *Handle) SetNavigationPicker(p NavigationPickerPort) {
	if h == nil {
		return
	}
	h.navigations = p
}

// navigationInspectorOptions 列出本工程全部菜单项（按位置分组排序）。
//
// 标签带位置前缀：同一个工程里「产品」这类标题在页眉与移动端各有一条，
// 只显示标题会让检查器里出现两个一模一样的选项，选错就静默绑到另一端的菜单上。
func navigationInspectorOptions(ctx context.Context, h *Handle, projectID, selected string) []inspectorOption {
	out := []inspectorOption{{Value: "", Label: "（不限）", Selected: selected == ""}}
	if h == nil || h.navigations == nil || projectID == "" {
		return out
	}
	rows, err := h.navigations.List(ctx, &navigationdto.ListReq{ProjectID: projectID})
	if err != nil {
		return out
	}
	for _, row := range rows {
		if row == nil || row.ID == "" {
			continue
		}
		// 只列根项：按项引用时渲染的是「该项及其子树」，挂到子项上也合法，
		// 但下拉里给全部项会让列表过长且层级难辨；子项可另用「按位置」模式取整棵树。
		if row.ParentID != nil && *row.ParentID != "" {
			continue
		}
		out = append(out, inspectorOption{
			Value:    row.ID,
			Label:    navigationKindLabel(row.Kind) + " · " + row.Title,
			Selected: row.ID == selected,
		})
	}
	return out
}

// navigationKindLabel 位置的中文名（与 admin 导航页的选项文案一致）。
func navigationKindLabel(kind string) string {
	switch kind {
	case "header":
		return "页眉"
	case "header_mobile":
		return "页眉（移动端）"
	case "footer":
		return "页脚"
	case "footer_mobile":
		return "页脚（移动端）"
	}
	return kind
}
