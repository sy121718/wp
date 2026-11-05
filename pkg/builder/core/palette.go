package core

import (
	"fmt"
)

const PaletteCategoryBasic = "basic"

type PaletteMeta struct {
	Type string `json:"type"`

	DisplayName string `json:"displayName"`

	Hint string `json:"hint"`

	Category string `json:"category"`

	DefaultProps map[string]any `json:"defaultProps,omitempty"`

	NeedsEntityContext bool `json:"-"`

	RequiredEntityTypes []string `json:"requiredEntityTypes,omitempty"`
}

type PaletteProvider interface {
	Palette() PaletteMeta
}

func ValidatePaletteMeta(typeName string, spec any, meta PaletteMeta) (err error) {
	if meta.DisplayName == "" {
		return fmt.Errorf("组件库显示名（DisplayName）不能为空")
	}
	switch meta.Category {
	case PaletteCategoryBasic:
	default:
		return fmt.Errorf("组件库分组 %q 未知（当前仅支持 %q）", meta.Category, PaletteCategoryBasic)
	}
	if meta.Type != "" && meta.Type != typeName {
		return fmt.Errorf("组件库元数据的 Type %q 与注册类型 %q 不一致", meta.Type, typeName)
	}
	if len(meta.DefaultProps) == 0 {
		return nil
	}
	names := jsonFieldNames(spec)
	for k := range meta.DefaultProps {
		if !names[k] {
			return fmt.Errorf("默认 Props 的字段 %q 不在组件 Props 中（键名拼错即拒绝）", k)
		}
	}
	return nil
}

func PaletteOf(typeName string) (meta PaletteMeta, ok bool) {
	comp, err := Lookup(typeName)
	if err != nil {
		return PaletteMeta{}, false
	}
	pp, isPalette := comp.(PaletteProvider)
	if !isPalette {
		return PaletteMeta{}, false
	}
	meta = pp.Palette()
	if meta.Type == "" {
		meta.Type = typeName
	}
	if meta.DisplayName == "" || meta.Category == "" {
		return PaletteMeta{}, false
	}
	return meta, true
}
