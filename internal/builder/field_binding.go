// field_binding.go — 实体字段绑定的收集与白名单校验（不变量 4，issue #6）。
//
// 背景：字段白名单由各领域模块（经实体类型注册表）维护，但「谁在什么时候校验」
// 此前只有构建期一条路 —— 解析器在渲染时拒绝越界字段，模板却能先存下来，
// 直到发布才报错。本文件把校验提前到「模板保存」这一步：组件经
// core.FieldBindingProvider 自报它声明的字段绑定，这里按注册表逐个核对。
//
// 校验口径（两个都查，缺一不可）：
//  1. 类型必须已注册（未知类型 = 数据源不存在）；
//  2. 声明的实体类型必须与**该绑定所属的数据源**一致 —— 商品模板里绑 article 字段
//     属于「商品数据源之外的绑定」，必须拒绝而不是构建期才炸。数据源是谁：普通组件
//     是文档的目标实体类型，集合组件是它声明的集合源实体类型（见 ValidateFieldRefs）；
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
//
// 集合作用域（审计 EDT-004 收口）：集合组件（core.productList / core.cardstack 等）
// 声明的绑定带上自己的集合源；它的**子树**同样落在该集合源的作用域里 ——
// cardstack 的「集合 + 子节点 = 子节点模板」模式按集合项逐项展开子树渲染
// （见 jetview.go 的 ItemScope），子节点绑的就是集合项字段。
func CollectFieldRefs(p *Page) (refs []core.FieldRef, err error) {
	if p == nil {
		return nil, nil
	}
	for _, n := range p.Root {
		sub, cerr := collectFieldRefsOfNode(n, "")
		if cerr != nil {
			return nil, cerr
		}
		refs = append(refs, sub...)
	}
	return refs, nil
}

// collectFieldRefsOfNode 递归收集单棵子树声明的字段绑定。
//
// collectionSource 是当前所在的集合作用域（空 = 不在任何集合里）：从集合组件那层起
// 向子树传递，嵌套集合组件用自己的源覆盖外层 —— 与渲染期的 ItemScope 嵌套同构。
func collectFieldRefsOfNode(n *core.Node, collectionSource string) (refs []core.FieldRef, err error) {
	if n == nil {
		return nil, nil
	}
	comp, lerr := core.Lookup(n.Type)
	if lerr != nil {
		// 未知组件类型由文档校验（ValidatePage）负责报错，这里不重复报。
		return nil, nil
	}
	if provider, ok := comp.(core.CollectionProvider); ok {
		if src := core.CollectionSourceOf(n, provider); src != "" {
			collectionSource = src
		}
	}
	if provider, ok := comp.(core.FieldBindingProvider); ok {
		sub, perr := provider.FieldBindings(n)
		if perr != nil {
			return nil, fmt.Errorf("节点 %s: %w", n.ID, perr)
		}
		// 组件自己已经声明了集合源时以它为准（它是这个绑定的直接来源）。
		if collectionSource != "" {
			for i := range sub {
				if sub[i].CollectionSource == "" {
					sub[i].CollectionSource = collectionSource
				}
			}
		}
		refs = append(refs, sub...)
	}
	for _, c := range n.Children {
		sub, cerr := collectFieldRefsOfNode(c, collectionSource)
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
//
// 目标数据源按**绑定自己的来源**判定（审计 EDT-004 收口）：
//
//   - 来自集合组件的绑定（FieldRef.CollectionSource 非空且是内容集合源）→ 集合源实体类型。
//     典型场景：商品分类归档模板（entity_type=product_category）里的 core.productList
//     绑 product.* —— 列表渲染的是商品集合，与模板实体不是同一个数据源；
//   - 其它（普通组件、集合组件尚未选源、插件集合源）→ 模板实体类型（既有行为不变）。
//
// **不是"只要有绑定就放行"**：两条判定（类型必须存在、字段必须在白名单内）照旧逐条执行，
// 只是"属于哪个数据源"这一条换了口径；非集合组件绑跨源字段仍然被拒。
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
		// 集合来源 → 只用集合源实体类型做判定；推不出实体类型（插件集合源）时
		// 退回模板实体类型 —— 宁可沿用严格口径，也不因为"是个集合"就整条跳过。
		target, fromCollection := entityType, false
		if src := core.CollectionEntityType(ref.CollectionSource); src != "" {
			target, fromCollection = src, true
		}
		if target != "" && ref.EntityType != target {
			if fromCollection {
				return fmt.Errorf("字段绑定 %s.%s 不属于集合源 %s 的数据源（跨数据源绑定被拒绝）",
					ref.EntityType, ref.Field, target)
			}
			return fmt.Errorf("字段绑定 %s.%s 不属于 %s 数据源（跨数据源绑定被拒绝）",
				ref.EntityType, ref.Field, target)
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
