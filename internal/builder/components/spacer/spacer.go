// Package spacer 实现 core.spacer 间隔组件——首个泛型基座（core.Atom）组件。
// 全部公共样板（ID 校验/叶子约束/props 解码/Advanced 校验与编译/class 织入）
// 由基座吸收，本文件只剩业务本体：三端高度属性 + 一个渲染函数（docs/02-C5）。
package spacer

import (
	_ "embed" // spacer.jet 经 //go:embed 打进二进制
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
	Height   Height             `json:"height,omitempty" ct:"rtext,sec=layout,label=高度"`
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
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
	core.RegisterTemplate("spacer", spacerTemplate)
}

// spacerTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed spacer.jet
var spacerTemplate string
