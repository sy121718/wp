package unit

// hover_touch_governance_test.go — 「依赖 :hover 的形态必须进 hover 桶、承载信息 / 操作的
// hover 必须给出触屏等价形态」这条硬规则（AGENTS.md 组件多端适配第三条）的回归检查，
// 对应审计 UI-005 / UI-006。
//
// 与 a11y_audit_test.go 同源思路：那份保证「每个组件的产物满足可访问性约束」，这份保证
// 「每个组件的 :hover 规则都在正确的桶里」。产物里出现裸 :hover 选择器就是漏治理 ——
// 触屏上它照样匹配（点一下把悬停态粘住），而依赖悬停展开的形态在触屏上等于不存在。
//
// 判据来自产物 CSS 的结构（CSSBuckets 输出）：
//   · hover 桶   → @media (hover: hover) { … }   （触屏上不输出）
//   · 触屏等价   → @media (hover: none)  { … }
//   · 按压反馈   → 裸 :active（不包媒体查询，触屏按下同样触发）

import (
	"strings"
	"testing"
)

// hoverGovernanceDocs 会真的产出 :hover 规则的组件文档（props 取最小可触发值）。
// 每条用例都必须产出至少一条 :hover 规则，否则用例本身是空转的（见测试里的断言）。
var hoverGovernanceDocs = []struct {
	name string
	doc  string
}{
	{"core.gallery", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"g1","type":"core.gallery","props":{"items":[{"url":"/a.jpg","alt":"图","caption":"图注"}],"captionMode":"hover","hover":{"scale":"1.05","overlay":"dark"}}}]}`},
	{"core.infobox", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.infobox","props":{"title":"标题","text":"正文","link":"/x","btnText":"了解更多","hoverBg":"#eeeeee"}}]}`},
	{"core.list", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"l1","type":"core.list","props":{"items":[{"text":"条目","link":"/x"}],"linkColorHover":"#f00","iconColorHover":"#0f0","iconBgColorHover":"#eef"}}]}`},
	{"core.languages", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"lg1","type":"core.languages","props":{"hoverColor":"#f00"}}]}`},
	{"core.marquee", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"m1","type":"core.marquee","props":{"pauseOnHover":true},"children":[{"id":"m1c","type":"core.text","props":{"mode":"plaintext","text":"跑马灯"}}]}]}`},
	{"core.breadcrumb", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"b1","type":"core.breadcrumb","props":{"homeLabel":"首页","items":[{"label":"栏目","url":"/c"}],"hoverColor":"#f00"}}]}`},
	{"core.nav", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.nav","props":{"items":[{"label":"首页","url":"/"}],"hoverColor":"#f00"}}]}`},
	{"core.tabs", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"t1","type":"core.tabs","props":{"tabs":[{"label":"一"},{"label":"二"}]},"children":[{"id":"t1a","type":"core.heading","props":{"text":"面板一","tag":"h3"}},{"id":"t1b","type":"core.heading","props":{"text":"面板二","tag":"h3"}}]}]}`},
	{"core.form", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"f1","type":"core.form","props":{"submitLabel":"提交","fields":[{"type":"text","label":"姓名","name":"name"}]}}]}`},
	{"core.image", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"im1","type":"core.image","props":{"src":"/a.jpg","alt":"图","hover":{"scale":"1.05"}}}]}`},
	{"core.container", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"c1","type":"core.container","props":{"tag":"section","styleEx":{"backgroundHover":"#eeeeee"},"layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"c1a","type":"core.heading","props":{"text":"子内容","tag":"h3"}}]}]}`},
}

// cssRule 产物里的一条伪类规则：选择器 + 所在环境。
type cssRule struct {
	selector string
	// inMedia 该规则被任意 @media 块包裹（按压反馈不该被包）。
	inMedia bool
	// bucketed 该规则落在 hover 桶里（(hover: hover) / (hover: none)）。
	bucketed bool
}

// frame 产物 CSS 的一层块：@media 层与 hover 桶层。
//
// 用显式块栈而不是「花括号深度」判断当前位置：产物整个包在 @layer sky-base 里，
// 深度有一个恒定的偏移，靠深度比较会把块外规则误判成块内（UI-008 的 :active 断言
// 就这样误报过一次）。
type cssFrame struct {
	media       bool
	hoverBucket bool
}

// scanRules 抽取产物里含某个伪类的规则，并标注它是否落在 @media / hover 桶里。
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

// TestComponentHoverRulesStayInBuckets 每个组件的 :hover 规则都必须包在 hover 桶里。
//
// UI-005：裸 :hover 在触屏上依然匹配，点一下就把悬停态粘住（粘滞 hover）。
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

// TestInteractiveHoverHasActiveFeedback 交互元素在触屏上的等价反馈是按压态（@active）。
//
// 触屏没有悬停，按压是唯一可靠的反馈来源 —— 审计 UI-005 的 impact 点名了
// 「form 的提交按钮与 tabs 的标签少了按压反馈，交互确认感弱」，gallery 缩略图同理。
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

// TestGalleryCaptionHasTouchFallback UI-006 的分类判据：**承载信息**的 hover 必须有触屏等价形态。
//
// 图集 captionMode=hover（图注悬停滑出）在触屏上没有等价形态就是信息不可达 ——
// 图注是内容，不是装饰，所以 (hover: none) 下必须常显。
// 反例：container 背景、image 微动、table 行高亮这类只改观感的 hover 不在此列
// （逐个组件的判断与理由写在各组件 CSS 的注释里）。
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
