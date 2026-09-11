// field_binding.go — 实体字段绑定的收集与白名单校验（不变量 4，issue #6）。
//
// 背景：字段白名单由各领域模块（经实体类型注册表）维护，但「谁在什么时候校验」
// 此前只有构建期一条路 —— 解析器在渲染时拒绝越界字段，模板却能先存下来，
// 直到发布才报错。本文件把校验提前到「模板保存」这一步：组件经
// core.FieldBindingProvider 自报它声明的字段绑定，这里按注册表逐个核对。
//
// 校验口径（两个都查，缺一不可）：
//  1. 类型必须已注册（未知类型 = 数据源不存在）；
//  2. 声明的实体类型必须与当前文档的目标实体类型一致 —— 商品模板里绑
//     article 字段属于「商品数据源之外的绑定」，必须拒绝而不是构建期才炸；
//  3. 字段必须在该类型的白名单内（白名单是唯一来源，见 core.EntityFieldSource）。
package builder

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// CollectFieldRefs 收集文档内全部组件声明的实体字段绑定（深度优先、声明序）。
//
// 只认实现了 core.FieldBindingProvider 的组件；未实现的组件（如 core.heading
// 的 binding.field）不在本次收集范围，其越界字段仍由构建期解析器拒绝。
func CollectFieldRefs(p *Page) (refs []core.FieldRef, err error) {
	if p == nil {
		return nil, nil
	}
	for _, n := range p.Root {
		sub, cerr := collectFieldRefsOfNode(n)
		if cerr != nil {
			return nil, cerr
		}
		refs = append(refs, sub...)
	}
	return refs, nil
}

// collectFieldRefsOfNode 递归收集单棵子树声明的字段绑定。
func collectFieldRefsOfNode(n *core.Node) (refs []core.FieldRef, err error) {
	if n == nil {
		return nil, nil
	}
	comp, lerr := core.Lookup(n.Type)
	if lerr != nil {
		// 未知组件类型由文档校验（ValidatePage）负责报错，这里不重复报。
		return nil, nil
	}
	if provider, ok := comp.(core.FieldBindingProvider); ok {
		sub, perr := provider.FieldBindings(n)
		if perr != nil {
			return nil, fmt.Errorf("节点 %s: %w", n.ID, perr)
		}
		refs = append(refs, sub...)
	}
	for _, c := range n.Children {
		sub, cerr := collectFieldRefsOfNode(c)
		if cerr != nil {
			return nil, cerr
		}
		refs = append(refs, sub...)
	}
	return refs, nil
}

// ValidateFieldRefs 按实体类型注册表校验文档内声明的字段绑定。
//
// entityType 为文档的目标实体类型（内容模板 / 发布实例的类型）；reg 为 nil 时
// 视为装配缺陷：宁可拒绝，也不静默放行白名单之外的绑定。
func ValidateFieldRefs(p *Page, entityType string, reg core.EntitySourceRegistry) (err error) {
	refs, err := CollectFieldRefs(p)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	if reg == nil {
		return fmt.Errorf("实体类型注册表未装配，无法校验字段绑定")
	}
	entityType = strings.TrimSpace(entityType)
	for _, ref := range refs {
		if ref.EntityType == "" || ref.Field == "" {
			return fmt.Errorf("字段绑定不完整：%q.%q", ref.EntityType, ref.Field)
		}
		if !reg.IsValidType(ref.EntityType) {
			return fmt.Errorf("未知的实体类型 %q（字段绑定 %s.%s）", ref.EntityType, ref.EntityType, ref.Field)
		}
		if entityType != "" && ref.EntityType != entityType {
			return fmt.Errorf("字段绑定 %s.%s 不属于 %s 数据源（跨数据源绑定被拒绝）",
				ref.EntityType, ref.Field, entityType)
		}
		if !fieldAllowed(reg.FieldWhitelist(ref.EntityType), ref.Field) {
			return fmt.Errorf("字段 %s.%s 不在数据源字段白名单内", ref.EntityType, ref.Field)
		}
	}
	return nil
}

// fieldAllowed 字段是否在该类型白名单内。
func fieldAllowed(whitelist []string, field string) bool {
	for _, f := range whitelist {
		if f == field {
			return true
		}
	}
	return false
}
