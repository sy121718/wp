package builder

import (
	"fmt"
	"strings"
)

type cssGuardExemption struct {
	Rule   string
	Match  string
	Reason string
}

var cssGuardExemptions = []cssGuardExemption{
	{
		Rule:  ruleHoverNoFallback,
		Match: "marquee",
		Reason: "跑马灯的悬停形态是「鼠标悬停时暂停滚动」（pauseOnHover，审计 UI-005 确认这条理由成立）：" +
			"触屏没有悬停这个动作，也就没有「一直悬停着把它按住」的诉求，补一个触屏等价形态（常驻暂停）" +
			"反而会让手机上的跑马灯永远不动 —— 规则在这里不适用，属于设计意图而非漏治理。",
	},
	{
		Rule:  ruleHoverNoFallback,
		Match: "components/accordion/accordion.css",
		Reason: "accordion 标题的悬停底色是装饰性定位辅助（审计 UI-006 已判定「不补触屏等价形态」）：" +
			"展开/收起是 <summary> 的原生点击行为，键盘与触屏都可用，展开态由 [open] 与 ＋/× 指示；" +
			"补一层按压反馈只会让触屏多一个不表达状态变化的假态。",
	},
	{
		Rule:  ruleHoverNoFallback,
		Match: "components/breadcrumb/breadcrumb.css",
		Reason: "面包屑链接的悬停只改文字颜色（可点是导航链接的固有属性，不靠悬停表达）：" +
			"触屏点一下会把悬停色粘在链接上，而这条规则要求补的「触屏等价形态」在这里没有对应物 ——" +
			"补常驻色等于让「可点」这个信息失效。",
	},
	{
		Rule:  ruleHoverNoFallback,
		Match: "components/container/container.css",
		Reason: "容器的悬停背景是装饰性反馈（审计 UI-006 已判定「不补触屏等价形态」）：" +
			"容器的操作入口是内部子组件（按钮/链接，它们各自有反馈），背景变色既不承载信息也不触发操作；" +
			"触屏缺这一层视觉变化不改变可达性。",
	},
	{
		Rule:  ruleHoverNoFallback,
		Match: "components/image/image.css",
		Reason: "图片的悬停缩放/滤镜是鼠标下的动效（审计 UI-006 已判定「不补触屏等价形态」）：" +
			"图片的交互入口是灯箱链接（:target 驱动，触屏照常可用）；" +
			"触屏没有悬停这个动作，补常驻缩放会改变版式，补按压反馈又会与灯箱点击冲突。",
	},
	{
		Rule:  ruleHoverNoFallback,
		Match: "components/languages/languages.css",
		Reason: "语言链接的悬停只改文字颜色：语言切换的「可点」由下划线与列表位置表达，" +
			"颜色不参与可达性；触屏上粘住的悬停色会让「当前语言」与「鼠标刚划过的语言」看起来一样，反而是误导。",
	},
	{
		Rule:  ruleHoverNoFallback,
		Match: "components/list/list.css",
		Reason: "列表链接的悬停只加下划线：链接的可点性由 <a> 元素本身与文字层级表达，" +
			"触屏没有悬停动作可等价；补常驻下划线等于把「悬停提示」变成「常驻样式」，改的是设计意图而不是适配。",
	},
	{
		Rule:  ruleHoverNoFallback,
		Match: "components/table/table.css",
		Reason: "表格行的悬停高亮是阅读辅助（审计 UI-006 已判定「不补触屏等价形态」）：" +
			"行不是点击目标（可点的是行内链接与按钮，它们自身有反馈），触屏用户靠斑马纹区分行；" +
			"给行补按压反馈会让「点行任意位置」看起来像有行为。",
	},
	{
		Rule:  ruleFixedWidth,
		Match: ".filter-field",
		Reason: "筛选行控件的绝对宽度是这条规则在本位置不适用的写法（theme.css 该区块注释已论证过）：" +
			".filter-field 是「label 固定 + 剩余空间给控件」的 flex 项，改成 min(100%, Npx) 会把下限与容器宽度" +
			"绑成互相依赖 —— 父等控件的固有尺寸、控件又等下界，单元被压窄时反而更容易把控件顶出单元。" +
			"现写法是绝对宽度 + min-width: 0（允许收缩到固有宽度以下）+ max-width: 100%（窄视口收口），" +
			"命中规则的是「值里是纯 px」这一形态，而规则要防的后果（撑宽文档）已被 max-width 兜住。",
	},
}

func applyCSSGuardExemptions(in []CSSViolation) (kept, exempted []CSSViolation) {
	for _, v := range in {
		if reason, ok := cssGuardExemptionReason(v); ok {
			v.Detail = v.Detail + "（已豁免：" + reason + "）"
			exempted = append(exempted, v)
			continue
		}
		kept = append(kept, v)
	}
	return kept, exempted
}

func cssGuardExemptionReason(v CSSViolation) (string, bool) {
	for _, ex := range cssGuardExemptions {
		if ex.Rule != "" && ex.Rule != v.Rule {
			continue
		}
		if ex.Match == "" {
			continue
		}
		if strings.Contains(v.Scope, ex.Match) || strings.Contains(v.File, ex.Match) || strings.Contains(v.Selector, ex.Match) {
			return ex.Reason, true
		}
	}
	return "", false
}

func validateCSSGuardExemptions() error {
	known := map[string]bool{
		ruleBareHover: true, ruleFixedWidth: true,
		ruleHoverNoFallback: true, ruleClampLower: true,
	}
	placeholders := []string{"todo", "fixme", "n/a", "na", "先这样", "暂时", "历史原因", "以后再说", "无"}
	for i, ex := range cssGuardExemptions {
		if ex.Rule != "" && !known[ex.Rule] {
			return fmt.Errorf("豁免第 %d 条：规则 id %q 不是已知规则", i+1, ex.Rule)
		}
		if strings.TrimSpace(ex.Match) == "" {
			return fmt.Errorf("豁免第 %d 条：匹配目标为空（豁免必须指向具体对象）", i+1)
		}
		reason := strings.TrimSpace(ex.Reason)
		if len([]rune(reason)) < 12 {
			return fmt.Errorf("豁免第 %d 条（%s）：理由太短，必须说明规则为何在此不适用", i+1, ex.Match)
		}
		lower := strings.ToLower(reason)
		for _, p := range placeholders {
			if strings.Contains(lower, p) {
				return fmt.Errorf("豁免第 %d 条（%s）：理由含占位词 %q，请写清具体原因", i+1, ex.Match, p)
			}
		}
	}
	return nil
}
