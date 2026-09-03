// Package spacer 实现 core.spacer 间隔组件——首个泛型基座（core.Atom）组件。
// 全部公共样板（ID 校验/叶子约束/props 解码/Advanced 校验与编译/class 织入）
// 由基座吸收，本文件只剩业务本体：三端高度属性 + 一个渲染函数（docs/02-C5）。
package spacer

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.spacer"

// Height 三端高度（CSS 长度值）。
type Height struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

// Props 间隔组件属性：三端高度 + Advanced 通用层（基座约定字段）。
type Props struct {
	Height   Height             `json:"height,omitempty"`
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 泛型基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName: Type,
		ValidateExtra: func(p *Props, nodeID string) error {
			for bp, v := range map[string]string{
				"desktop": p.Height.Desktop, "tablet": p.Height.Tablet, "mobile": p.Height.Mobile,
			} {
				if v != "" && !core.IsSafeCSSValue(v) {
					return fmt.Errorf("无效的 %s 端高度: %q", bp, v)
				}
			}
			return nil
		},
	},
}

// init 注册组件。
func init() {
	core.Register(Widget)
}
