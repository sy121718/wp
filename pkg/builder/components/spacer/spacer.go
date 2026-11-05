package spacer

import (
	_ "embed"
	"fmt"

	"go_wp/pkg/builder/core"
)

const Type = "core.spacer"

type Height struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

type Props struct {
	Height   Height             `json:"height,omitempty" ct:"rtext,sec=layout,label=高度"`
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "间隔",
		Hint:            "留白空间",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"height": map[string]any{
				"desktop": "32px",
			},
		},
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

func init() {
	core.Register(Widget)
	core.RegisterTemplate("spacer", spacerTemplate)
}

//go:embed spacer.jet
var spacerTemplate string
