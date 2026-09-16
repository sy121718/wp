package core

// palette.go — 组件库呈现元数据（审计 REG-005）。
//
// 定位：组件的可编辑属性 schema（ct 标签 → ComponentSchemas）已经是单一真源；
// 但组件在编辑器组件库里的呈现元数据（分类、显示名、插入时的默认 Props）过去
// 手工写在前端 palette.js，与 Go 侧分离 —— 新增组件要改两处，漏改则组件在编辑器
// 里不可见（与 REG-001 的双清单是同一类问题）。本文件把这部分上移到组件声明侧，
// palette.js 只消费注入数据 + 保留自动化不了的人工信息（分组排序、结构型默认子树）。
//
// 声明方式（组件作者）：
//
//	Atom 基座组件：core.AtomSpec[Props]{
//	    TypeName: Type, DisplayName: "容器", Hint: "布局容器",
//	    PaletteCategory: core.PaletteCategoryBasic,
//	    DefaultProps: map[string]any{"tag": "section"},
//	}
//	自定义结构组件：func (c *Component) Palette() PaletteMeta { ... }
//
// 不进组件库的组件（core.globalref / core.layoutSlot —— 由专门路径产生，静态条目
// 给不出它们需要的上下文）不声明本元数据；「哪些组件可以不上组件库」由
// builder 侧覆盖断言的显式豁免表声明，而不是靠「忘了写」静默通过。

import (
	"fmt"
)

// PaletteCategoryBasic 基础组件分组键。
//
// 当前只有这一个分组（细分留给将来的进阶组件）；分组顺序与分组标题属于
// 「自动化不了的人工信息」，留在前端工作台的排序表里。
const PaletteCategoryBasic = "basic"

// PaletteMeta 组件在编辑器组件库里的呈现元数据。
//
// 它是「组件怎么出现在组件库里」的唯一 Go 侧声明：工作台前端经生成的
// generated-contracts.js 消费，不再手写组件条目。
type PaletteMeta struct {
	// Type 组件类型标识（与 Component.Type() 一致）。
	Type string `json:"type"`
	// DisplayName 组件库显示名（中文，作者可见）。
	DisplayName string `json:"displayName"`
	// Hint 组件库一句话说明（显示在显示名下方）。
	Hint string `json:"hint"`
	// Category 分组键（见 PaletteCategory* 常量）。
	Category string `json:"category"`
	// DefaultProps 插入时的默认 Props（示例内容，保证「插入即合法」）：
	// 键必须在组件 Props 的 JSON 字段集合内（注册期校验，拼错即 panic）；
	// nil 表示无需默认值（组件插入时 props 为空对象）。
	DefaultProps map[string]any `json:"defaultProps,omitempty"`
}

// PaletteProvider 由「出现在组件库里」的组件实现：返回组件库呈现元数据。
//
// Atom 基座自动实现（从 AtomSpec 取）；自定义结构组件就近声明。
// 未实现本接口、或元数据不完整（DisplayName / Category 为空）的组件不进组件库 ——
// builder 的覆盖断言要求每个注册组件「要么有元数据、要么在显式豁免表里」。
type PaletteProvider interface {
	Palette() PaletteMeta
}

// ValidatePaletteMeta 校验组件声明的组件库元数据（注册期调用，fail-fast）。
//
// spec 为组件 Props 零值指针（SpecProvider.PropsSpec()，可为 nil）：
// DefaultProps 的键必须存在于 Props 的 JSON 字段集合，拼错即拒绝 ——
// 与可翻译白名单同一套核对方式（见 translatable.go），不另建字符串表。
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

// PaletteOf 返回组件声明的组件库元数据（未声明 / 未注册 / 元数据不完整返回 false）。
//
// 「元数据不完整」不在这里 panic：Atom 基座组件天然实现 PaletteProvider，
// 未声明元数据时返回的是零值。此处按「能不能上组件库」判定（显示名 + 分组齐全）。
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
