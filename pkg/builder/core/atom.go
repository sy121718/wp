package core

import (
	"fmt"
	"reflect"
)

type AtomSpec[P any] struct {
	TypeName string

	ValidateExtra func(p *P, nodeID string) error

	CollectionProp string

	Translatable []string

	DisplayName string

	Hint string

	PaletteCategory string

	DefaultProps map[string]any

	PaletteEntityContext bool
}

type Atom[P any] struct{ Spec AtomSpec[P] }

func (a Atom[P]) Type() string { return a.Spec.TypeName }

func (a Atom[P]) PropsSpec() any { var p P; return &p }

func (a Atom[P]) CollectionProp() string { return a.Spec.CollectionProp }

func (a Atom[P]) Translatable() []string { return a.Spec.Translatable }

func (a Atom[P]) Palette() PaletteMeta {
	return PaletteMeta{
		Type:               a.Spec.TypeName,
		DisplayName:        a.Spec.DisplayName,
		Hint:               a.Spec.Hint,
		Category:           a.Spec.PaletteCategory,
		DefaultProps:       a.Spec.DefaultProps,
		NeedsEntityContext: a.Spec.PaletteEntityContext,
	}
}

func (a Atom[P]) Validate(node *Node, ids map[string]bool) (err error) {
	if err = ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 原子组件为叶子节点，不允许子节点", node.ID)
	}

	p, derr := DecodeProps[P](node.Props)
	if derr != nil {
		return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, derr)
	}
	if err = ValidateSpec(&p, node.ID); err != nil {
		return err
	}

	if adv := AdvancedOf(&p); adv != nil {
		if err = ValidateAdvanced(adv, node.ID, ids); err != nil {
			return err
		}
	}
	if a.Spec.ValidateExtra != nil {
		if err = a.Spec.ValidateExtra(&p, node.ID); err != nil {
			return fmt.Errorf("节点 %s: %w", node.ID, err)
		}
	}
	return nil
}

func AdvancedOf[P any](p *P) *AdvancedProps {
	v := reflect.ValueOf(p)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName("Advanced")
	if !f.IsValid() || f.Type() != reflect.TypeOf(AdvancedProps{}) {
		return nil
	}
	if !f.CanAddr() {
		return nil
	}
	return f.Addr().Interface().(*AdvancedProps)
}

func ValidateNodeID(id, name string, ids map[string]bool) (err error) {
	if len(id) < 1 || len(id) > 64 {
		return fmt.Errorf("无效的节点 ID: %q", id)
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return fmt.Errorf("无效的节点 ID: %q", id)
		}
	}
	if ids[id] {
		return fmt.Errorf("节点 ID 重复: %q", id)
	}
	ids[id] = true
	if err = ValidateNodeName(name); err != nil {
		return err
	}
	return nil
}

func ValidateNodeName(name string) (err error) {
	if len([]rune(name)) > 100 {
		return fmt.Errorf("节点名称过长（上限 100 字符）")
	}
	return nil
}
