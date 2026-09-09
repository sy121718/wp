// Package core 是可视化构建器的编译内核：组件树节点结构、组件接口与注册表。
//
// 组件（core.container、后续的 heading/text 等）实现 Component 接口并注册到 Registry，
// 编译器按节点 type 查找对应组件完成校验与渲染。一个组件一个目录，见 components/。
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
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
//
// 说明：HTML 渲染已迁移到 Jet 模板路径（builder/jetview.go 的 nodeViewOf + renderView），
// 组件不再承担 HTML 拼装，故接口只保留 Type + Validate 两项。
type Component interface {
	// Type 组件类型标识，如 "core.container"。
	Type() string
	// Validate 校验节点（含解码并校验自身 props、递归校验子树）。
	Validate(node *Node, ids map[string]bool) error
}

// registry 组件注册表。
var registry = map[string]Component{}

// Register 注册组件。重复类型直接覆盖（便于测试替换），生产组件在包 init 中注册。
//
// 注册期校验可翻译字段白名单（多语言 P5b，docs/06-D §7.5 规则 2）：
// 组件声明了 Translatable 时，字段名必须合法且存在于自身 Props 的 JSON 字段集合，
// 拼错即 panic（init 期 fail-fast）——白名单是唯一可翻译性来源，不允许静默失效。
func Register(c Component) {
	if c == nil {
		panic("core.Register: 组件为 nil")
	}
	if tp, ok := c.(TranslatableProvider); ok {
		var spec any
		if sp, ok := c.(SpecProvider); ok {
			spec = sp.PropsSpec()
		}
		if err := ValidateTranslatable(spec, tp.Translatable()); err != nil {
			panic(fmt.Sprintf("组件 %s 可翻译字段白名单非法: %v", c.Type(), err))
		}
	}
	registry[c.Type()] = c
	delete(translatableCache, c.Type())
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

// ErrIncompleteNode 标记「节点配置不完整」（如轮播未拖入 slide）——
// 属于编辑中间态，可由容错校验跳过该节点而非阻断整页；
// 与「配置非法」（非法 props / 属性 key / 超长名称）区分，后者必须拒绝。
var ErrIncompleteNode = errors.New("节点配置不完整")

// ValidateNode 校验单个节点：按类型分发到已注册组件。
// plugin.* 前缀为运行时插件组件（registry 不感知），只做结构校验
// （ID 唯一/无子节点）；props 值校验在渲染装配（builder.decodePluginProps，
// 白名单与 spec 同源），未安装/未启用的插件节点在编译期报明确错误。
func ValidateNode(node *Node, ids map[string]bool) (err error) {
	if node == nil {
		return fmt.Errorf("节点为空")
	}
	if strings.HasPrefix(node.Type, "plugin.") {
		if err = ValidateNodeID(node.ID, node.Name, ids); err != nil {
			return err
		}
		if len(node.Children) > 0 {
			return fmt.Errorf("节点 %s: 插件组件不支持子节点", node.ID)
		}
		return nil
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
