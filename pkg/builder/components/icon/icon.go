package icon

import (
	_ "embed"
	"fmt"

	"go_wp/pkg/builder/core"
)

const Type = "core.icon"

const defaultIconName = "star"

var iconNames = map[string]string{
	"star":        "star",
	"heart":       "heart",
	"check":       "check",
	"arrow-right": "arrow-right",
	"arrow-left":  "arrow-left",
	"info":        "info",
	"close":       "x",
	"search":      "search",
}

func iconPath(name string) (string, bool) {
	lib, ok := iconNames[name]
	if !ok {
		return "", false
	}
	return core.IconInner(lib)
}

type Props struct {
	IconName string `json:"iconName,omitempty" ct:"select,star=星形,heart=心形,check=对勾,arrow-right=右箭头,arrow-left=左箭头,info=信息,close=关闭,search=搜索,default=star,sec=content,label=图标样式"`

	Size string `json:"size,omitempty" ct:"dimension,maxlen=20,sec=style,label=尺寸"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "图标",
		Hint:            "通用 SVG 图标",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"iconName": "star",
			"size":     "24px",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
	if p.IconName != "" {
		if _, ok := iconNames[p.IconName]; !ok {
			return fmt.Errorf("无效的内置图标: %q（仅 star/heart/check/arrow-right/arrow-left/info/close/search）", p.IconName)
		}
	}
	return nil
}

//go:embed icon.css
var iconCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{"size": "1.5em", "color": "currentColor"}
	if p.Size != "" {
		vars["size"] = p.Size
	}
	if p.Color != "" {
		vars["color"] = p.Color
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, iconCSS, vars); err != nil {

		panic(fmt.Sprintf("icon 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("icon", iconTemplate)
}

//go:embed icon.jet
var iconTemplate string
