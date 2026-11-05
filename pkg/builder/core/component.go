package core

import (
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
)

type Node struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Props    json.RawMessage         `json:"props"`
	Children []*Node                 `json:"children"`
	Actions  map[string]ActionTarget `json:"actions,omitempty"`

	Name string `json:"name,omitempty"`

	Hidden bool `json:"hidden,omitempty"`

	Locked bool `json:"locked,omitempty"`
}

const SectionClass = "sky-section"

const SROnlyClass = "sky-sr-only"

type Component interface {
	Type() string

	Validate(node *Node, ids map[string]bool) error
}

var registry = map[string]Component{}

func Register(c Component) {
	if c == nil {
		panic("core.Register: 组件为 nil")
	}
	if rp, ok := c.(AlignedRepeaterProvider); ok {
		var props any
		if sp, ok := c.(SpecProvider); ok {
			props = sp.PropsSpec()
		}
		if err := ValidateAlignedRepeater(props, rp.AlignedRepeater()); err != nil {
			panic(fmt.Sprintf("组件 %s 对齐面板契约非法: %v", c.Type(), err))
		}
	}
	if pp, ok := c.(PaletteProvider); ok {
		var spec any
		if sp, ok := c.(SpecProvider); ok {
			spec = sp.PropsSpec()
		}
		meta := pp.Palette()

		if meta.DisplayName != "" || meta.Category != "" {
			if err := ValidatePaletteMeta(c.Type(), spec, meta); err != nil {
				panic(fmt.Sprintf("组件 %s 组件库元数据非法: %v", c.Type(), err))
			}
		}
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

	resetTranslatableFields(c.Type())
	resetTranslatableMetaByType(c.Type())
}

func Lookup(typeName string) (c Component, err error) {
	c, ok := registry[typeName]
	if !ok {
		return nil, fmt.Errorf("不支持的组件类型: %s", typeName)
	}
	return c, nil
}

func Types() (types []string) {
	types = make([]string, 0, len(registry))
	for name := range registry {
		types = append(types, name)
	}
	sort.Strings(types)
	return types
}

var ErrIncompleteNode = errors.New("节点配置不完整")

type selfValidator interface {
	ValidateSelf(node *Node, ids map[string]bool) error
}

func ValidateNode(node *Node, ids map[string]bool) error {
	return validateNode(node, ids, nil)
}

func ValidateNodeTolerant(node *Node, ids map[string]bool, incomplete map[string]bool) error {
	return validateNode(node, ids, incomplete)
}

func validateNode(node *Node, ids map[string]bool, incomplete map[string]bool) error {
	if node == nil {
		return fmt.Errorf("节点为空")
	}
	if strings.HasPrefix(node.Type, "plugin.") {
		if len(node.Actions) > 0 {
			return fmt.Errorf("节点 %s: 插件未声明动作", node.ID)
		}
		if err := ValidateNodeID(node.ID, node.Name, ids); err != nil {
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
	local, hasChildren := comp.(selfValidator)
	if hasChildren {
		err = local.ValidateSelf(node, ids)
	} else {
		err = comp.Validate(node, ids)
	}
	if err != nil {
		if incomplete == nil || !errors.Is(err, ErrIncompleteNode) {
			return err
		}
		incomplete[node.ID] = true
	}
	if actionErr := ValidateNodeActions(node, comp); actionErr != nil {
		if incomplete == nil || !errors.Is(actionErr, ErrIncompleteNode) {
			return actionErr
		}
		incomplete[node.ID] = true
	}
	if hasChildren {
		return validateChildren(node, ids, incomplete)
	}
	return nil
}

func ValidateChildren(node *Node, ids map[string]bool) error {
	return validateChildren(node, ids, nil)
}

func validateChildren(node *Node, ids map[string]bool, incomplete map[string]bool) error {
	for i, child := range node.Children {
		if err := validateNode(child, ids, incomplete); err != nil {
			return fmt.Errorf("节点 %s 子节点 %d: %w", node.ID, i, err)
		}
	}
	return nil
}

type SpecProvider interface {
	PropsSpec() any
}
