package unit

import (
	"strings"
	"testing"
)

var hoverGovernanceDocs = []struct {
	name string
	doc  string
}{
	{"core.gallery", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"g1","type":"core.gallery","props":{"items":[{"url":"/storage/a.jpg","alt":"图","caption":"图注"}],"captionMode":"hover","hover":{"scale":"1.05","overlay":"dark"}}}]}`},
	{"core.infobox", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.infobox","props":{"title":"标题","text":"正文","link":"/x","btnText":"了解更多","hoverBg":"#eeeeee"}}]}`},
	{"core.list", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"l1","type":"core.list","props":{"items":[{"text":"条目","link":"/x"}],"linkColorHover":"#f00","iconColorHover":"#0f0","iconBgColorHover":"#eef"}}]}`},
	{"core.languages", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"lg1","type":"core.languages","props":{"hoverColor":"#f00"}}]}`},
	{"core.marquee", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"m1","type":"core.marquee","props":{"pauseOnHover":true},"children":[{"id":"m1c","type":"core.text","props":{"mode":"plaintext","text":"跑马灯"}}]}]}`},
	{"core.breadcrumb", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"b1","type":"core.breadcrumb","props":{"homeLabel":"首页","items":[{"label":"栏目","url":"/c"}],"hoverColor":"#f00"}}]}`},
	{"core.nav", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.nav","props":{"items":[{"label":"首页","url":"/"}],"hoverColor":"#f00"}}]}`},
	{"core.tabs", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"t1","type":"core.tabs","props":{"tabs":[{"label":"一"},{"label":"二"}]},"children":[{"id":"t1a","type":"core.heading","props":{"text":"面板一","tag":"h3"}},{"id":"t1b","type":"core.heading","props":{"text":"面板二","tag":"h3"}}]}]}`},
	{"core.form", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"f1","type":"core.form","props":{"submitLabel":"提交","fields":[{"type":"text","label":"姓名","name":"name"}]}}]}`},
	{"core.image", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"im1","type":"core.image","props":{"src":"/storage/a.jpg","alt":"图","hover":{"scale":"1.05"}}}]}`},
	{"core.container", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c1","type":"core.container","props":{"tag":"section","styleEx":{"backgroundHover":"#eeeeee"},"layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"c1a","type":"core.heading","props":{"text":"子内容","tag":"h3"}}]}]}`},
}

type cssRule struct {
	selector string

	inMedia bool

	bucketed bool
}

type cssFrame struct {
	media       bool
	hoverBucket bool
}

func scanRules(css, pseudo string) []cssRule {
	var out []cssRule
	var stack []cssFrame
	anyFrame := func(pred func(cssFrame) bool) bool {
		for _, f := range stack {
			if pred(f) {
				return true
			}
		}
		return false
	}
	for _, raw := range strings.Split(css, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		opens := strings.Count(line, "{")
		closes := strings.Count(line, "}")
		if opens > 0 && strings.Contains(line, pseudo) {
			out = append(out, cssRule{
				selector: line,
				inMedia:  anyFrame(func(f cssFrame) bool { return f.media }),
				bucketed: anyFrame(func(f cssFrame) bool { return f.hoverBucket }),
			})
		}
		for i := 0; i < opens-closes; i++ {
			stack = append(stack, cssFrame{
				media: strings.HasPrefix(line, "@media"),
				hoverBucket: strings.HasPrefix(line, "@media") &&
					(strings.Contains(line, "(hover: hover)") || strings.Contains(line, "(hover: none)")),
			})
		}
		for i := 0; i < closes-opens; i++ {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out
}

func TestComponentHoverRulesStayInBuckets(t *testing.T) {
	total := 0
	for _, c := range hoverGovernanceDocs {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, css := compileDoc(t, c.doc)
			rules := scanRules(css, ":hover")
			if len(rules) == 0 {
				t.Fatalf("%s 没有产出任何 :hover 规则（用例失效，props 需要补全）", c.name)
			}
			total += len(rules)
			for _, r := range rules {
				if !r.bucketed {
					t.Errorf("裸 :hover 规则未进 hover 桶（触屏上会粘滞）：%s", r.selector)
				}
			}
		})
	}
	t.Logf("覆盖 %d 条 :hover 规则 / %d 个组件", total, len(hoverGovernanceDocs))
}

func TestInteractiveHoverHasActiveFeedback(t *testing.T) {
	for _, c := range hoverGovernanceDocs {
		switch c.name {
		case "core.tabs", "core.form", "core.gallery":
		default:
			continue
		}
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, css := compileDoc(t, c.doc)
			rules := scanRules(css, ":active")
			if len(rules) == 0 {
				t.Fatalf("%s 缺少按压反馈（触屏没有悬停，按压是唯一反馈路径）", c.name)
			}
			for _, r := range rules {
				if r.inMedia {
					t.Errorf("按压规则被包进了媒体查询（触屏上失效）：%s", r.selector)
				}
			}
		})
	}
}

func TestGalleryCaptionHasTouchFallback(t *testing.T) {
	_, css := compileDoc(t, hoverGovernanceDocs[0].doc)
	idx := strings.Index(css, "@media (hover: none)")
	if idx < 0 {
		t.Fatalf("图注 hover 模式没有触屏等价形态（触屏上看不到图注）：\n%s", css)
	}
	block := css[idx:]
	if end := strings.Index(block, "\n}"); end > 0 {
		block = block[:end]
	}
	if !strings.Contains(block, "figcaption") || !strings.Contains(block, "opacity: 1") {
		t.Errorf("触屏块里图注没有常显：\n%s", block)
	}
}
