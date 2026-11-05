package countdown

import (
	_ "embed"
	"fmt"
	"time"

	"go_wp/pkg/builder/core"
)

//go:embed enhance.js
var enhanceJS string

const Type = "core.countdown"

var targetDateLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

type Props struct {
	TargetDate string `json:"targetDate,omitempty" ct:"text,maxlen=30,sec=content,label=目标时间"`

	ShowDays bool `json:"showDays,omitempty" ct:"bool,sec=content,label=显示天数"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "倒计时",
		Hint:            "营销倒计时",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"targetDate": "2030-01-01 00:00:00",
			"showDays":   true,
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
	if p.TargetDate == "" {
		return fmt.Errorf("必须提供目标时间")
	}
	if _, ok := parseTargetDate(p.TargetDate); !ok {
		return fmt.Errorf("无效的目标时间: %q（支持 RFC3339 或 2006-01-02 15:04:05）", p.TargetDate)
	}
	return nil
}

func parseTargetDate(s string) (time.Time, bool) {
	for _, layout := range targetDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

//go:embed countdown.css
var countdownCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, countdownCSS); err != nil {

		panic(fmt.Sprintf("countdown 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("countdown", countdownTemplate)
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCountdowns"},
		Feats:  []string{"data-countdown"},
		Source: enhanceJS,
	})
}

//go:embed countdown.jet
var countdownTemplate string
