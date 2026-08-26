// Package core 是可视化构建器的编译内核：组件树节点结构、组件接口与注册表。
//
// 组件（core.container、后续的 heading/text 等）实现 Component 接口并注册到 Registry，
// 编译器按节点 type 查找对应组件完成校验与渲染。一个组件一个目录，见 components/。
package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Node 组件树节点通用结构。Props 由各组件自行解码为自己的 props 类型。
type Node struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props"`
	Children []*Node         `json:"children"`
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
	Render(node *Node, topLevel bool, html *strings.Builder, css *CSSBuckets) error
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

// RenderNode 渲染单个节点：按类型分发到已注册组件。
func RenderNode(node *Node, topLevel bool, html *strings.Builder, css *CSSBuckets) (err error) {
	if node == nil {
		return fmt.Errorf("节点为空")
	}
	comp, err := Lookup(node.Type)
	if err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	return comp.Render(node, topLevel, html, css)
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