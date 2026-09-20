package core

// collection_deps.go — 内置组件的集合源 → 构建依赖（审计 ARCH-01）。
//
// 背景：集合类组件（core.productList / core.cardstack）在 Props 里声明「渲染哪个集合源」，
// 但依赖登记此前只认**插件 manifest** 声明的集合绑定（plugincomp 的 spec.Collection），
// 内置组件一条都不登记 —— 于是「新增一个商品」不会让任何列表页失效，
// 产物一直停在旧字节，日志里什么都没有。
//
// 本文件是那份登记的唯一来源：组件自己声明集合源字段名（CollectionProvider），
// 这里按同一份声明从文档树里读出来。调用方（page 的依赖推导、presentation 的依赖登记）
// 不再各自维护「类型 → 集合源」的映射表 —— 多一份表就多一次静默漂移。
//
// 与插件路径的关系：**并集**，不是替代。插件组件不在本包注册表里（它们由
// plugincontract.Assembly 解析），插件那一路继续由调用方按 asm.Specs 处理。

import (
	"encoding/json"
	"sort"
	"strings"
)

// CollectionProvider 组件声明「本组件从某个 Props 字段读取集合源」。
//
// 取值为集合源标识（如 content:product），空串 = 本节点没选集合源（静态形态）。
type CollectionProvider interface {
	// CollectionProp 返回承载集合源标识的 Props JSON 字段名（如 "collectionSource"）。
	CollectionProp() string
}

// CollectionSourcesOf 收集文档树里内置组件**声明消费**的集合源（去重、升序）。
//
// 与 builder.ReferencedBlockIDs 同形：只回答「这份文档声明了哪些集合源」，
// 不做任何业务判断。未注册的类型（插件组件、历史遗留类型）一律跳过。
func CollectionSourcesOf(roots []*Node) []string {
	if len(roots) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if c, ok := registry[n.Type]; ok {
			if p, ok := c.(CollectionProvider); ok {
				if src := collectionSourcePropValue(n.Props, p.CollectionProp()); src != "" && !seen[src] {
					seen[src] = true
					out = append(out, src)
				}
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	sort.Strings(out)
	return out
}

// collectionSourcePropValue 按字段名从节点 Props 读取集合源标识（非法 JSON / 缺失返回空串）。
func collectionSourcePropValue(raw json.RawMessage, prop string) string {
	prop = strings.TrimSpace(prop)
	if len(raw) == 0 || prop == "" {
		return ""
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		return ""
	}
	s, ok := props[prop].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
