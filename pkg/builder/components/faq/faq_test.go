package faq

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func manyItems(n int) []FaqItem {
	items := make([]FaqItem, n)
	for i := range items {
		items[i] = FaqItem{Question: "问题", Answer: "答案"}
	}
	return items
}

func TestValidateExtra(t *testing.T) {
	tests := []struct {
		name    string
		props   *Props
		wantErr bool
	}{
		{"空条目非法", &Props{}, true},
		{"单条合法", &Props{Items: []FaqItem{{Question: "问题", Answer: "答案"}}}, false},
		{"默认展开合法", &Props{Items: []FaqItem{{Question: "问题", Answer: "答案", Open: true}}}, false},
		{"缺问题", &Props{Items: []FaqItem{{Answer: "答案"}}}, true},
		{"缺答案", &Props{Items: []FaqItem{{Question: "问题"}}}, true},
		{"条数上限内", &Props{Items: manyItems(maxFaqItems)}, false},
		{"条数超上限", &Props{Items: manyItems(maxFaqItems + 1)}, true},
		{"问题超长", &Props{Items: []FaqItem{{Question: strings.Repeat("问", maxQuestionLen+1), Answer: "答案"}}}, true},
		{"答案超长", &Props{Items: []FaqItem{{Question: "问题", Answer: strings.Repeat("答", maxAnswerLen+1)}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExtra(tt.props, "n1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExtra(%+v) err=%v, wantErr=%v", tt.props, err, tt.wantErr)
			}
		})
	}
}

func TestCompileCSS(t *testing.T) {
	b := &core.CSSBuckets{}
	compileCSS("n1", &Props{Items: []FaqItem{{Question: "问题", Answer: "答案"}}}, b)
	css := b.String()
	for _, want := range []string{
		"display: flex", "flex-direction: column",
		" details", " summary", "details[open] .sky-faq-chevron", ".sky-faq-answer",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, css)
		}
	}
}
