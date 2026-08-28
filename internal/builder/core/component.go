// Package core 是可视化构建器的编译内核：组件树节点结构、组件接口与注册表。
//
// 组件（core.container、后续的 heading/text 等）实现 Component 接口并注册到 Registry，
// 编译器按节点 type 查找对应组件完成校验与渲染。一个组件一个目录，见 components/。
package core

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Node 组件树节点通用结构。Props 由各组件自行解码为自己的 props 类型。
type Node struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props"`
	Children []*Node         `json:"children"`

	// --- 编辑元数据（Visual Workbench 03-A，持久化于 Page Document，不参与编译产物） ---

	// Name 大纲树重命名显示名（如 "首屏 Banner 容器"），仅编辑器可读性。
	Name string `json:"name,omitempty"`
	// Hidden 编辑期临时显隐（遮挡编辑辅助），不影响最终发布产物。
	Hidden bool `json:"hidden,omitempty"`
	// Locked 编辑期锁定防误触（禁止画布选中/拖拽）。
	Locked bool `json:"locked,omitempty"`
}

// SectionClass 顶级容器附加 class，用于页面版心约束选择器。
const SectionClass = "wp-section"

// Component 组件接口。每种可视化组件实现本接口并注册到 Registry。
type Component interface {
	// Type 组件类型标识，如 "core.container"。
	Type() string
	// Validate 校验节点（含解码并校验自身 props、递归校验子树）。
	Validate(node *Node, ids map[string]bool) error
	// Render 渲染节点 HTML 并编译 CSS。topLevel 表示是否为页面第一层顶级 Section。
	Render(node *Node, topLevel bool, ctx *RenderContext) error
}

// registry 组件注册表。
var registry = map[string]Component{}

// Register 注册组件。重复类型直接覆盖（便于测试替换），生产组件在包 init 中注册。
func Register(c Component) {
	registry[c.Type()] = c
}

// Lookup 按类型查找组件。
func Lookup(typeName string) (c Component, err error) {
	c, ok := registry[typeName]
	if !ok {
		return nil, fmt.Errorf("不支持的组件类型: %s", typeName)
	}
	return c, nil
}

// Types 返回注册表全部组件类型标识（字典序，确定性输出）。
// 供编辑器侧一次性拉取组件元数据（Inspector 面板 schema）。
func Types() (types []string) {
	types = make([]string, 0, len(registry))
	for name := range registry {
		types = append(types, name)
	}
	sort.Strings(types)
	return types
}

// RenderNode 渲染单个节点：按类型分发到已注册组件。
func RenderNode(node *Node, topLevel bool, ctx *RenderContext) (err error) {
	if node == nil {
		return fmt.Errorf("节点为空")
	}
	comp, err := Lookup(node.Type)
	if err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	return comp.Render(node, topLevel, ctx)
}

// ValidateNode 校验单个节点：按类型分发到已注册组件。
func ValidateNode(node *Node, ids map[string]bool) (err error) {
	if node == nil {
		return fmt.Errorf("节点为空")
	}
	comp, err := Lookup(node.Type)
	if err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	return comp.Validate(node, ids)
}

// SpecProvider 可选接口：组件通过 PropsSpec() 暴露一个带 ct tag 的 props 结构体，
// 声明式 Controls 据此自动生成校验与 Inspector 面板 schema（docs/02-C3）。
// 未实现本接口的组件保留手写校验（兼容阶段）。
type SpecProvider interface {
	PropsSpec() any
}
