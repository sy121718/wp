package faq

import (
	_ "embed"
	"fmt"

	"go_wp/internal/builder/core"
)

const Type = "core.faq"

const (
	maxFaqItems    = 50
	maxQuestionLen = 200
	maxAnswerLen   = 2000
)

type FaqItem struct {
	Question string `json:"question"`

	Answer string `json:"answer" ct:"richtext,maxlen=2000,label=答案"`

	Open bool `json:"open,omitempty"`
}

type Props struct {
	Items []FaqItem `json:"items,omitempty"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "常见问题",
		Hint:            "问答折叠",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"items": []any{
				map[string]any{
					"question": "常见问题一？",
					"answer":   "这里是回答内容。",
					"open":     true,
				},
				map[string]any{
					"question": "常见问题二？",
					"answer":   "这里是回答内容。",
				},
			},
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"question", "answer"},
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
	if len(p.Items) == 0 {
		return fmt.Errorf("常见问题至少需要一条")
	}
	if len(p.Items) > maxFaqItems {
		return fmt.Errorf("常见问题条数超上限（%d）: %d", maxFaqItems, len(p.Items))
	}
	for i, it := range p.Items {
		if it.Question == "" {
			return fmt.Errorf("第 %d 条缺少问题", i+1)
		}
		if len(it.Question) > maxQuestionLen {
			return fmt.Errorf("第 %d 条问题超长（上限 %d）", i+1, maxQuestionLen)
		}
		if it.Answer == "" {
			return fmt.Errorf("第 %d 条缺少答案", i+1)
		}
		if len(it.Answer) > maxAnswerLen {
			return fmt.Errorf("第 %d 条答案超长（上限 %d）", i+1, maxAnswerLen)
		}
	}
	return nil
}

//go:embed faq.css
var faqCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, faqCSS); err != nil {

		panic(fmt.Sprintf("faq 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("faq", faqTemplate)
}

//go:embed faq.jet
var faqTemplate string
