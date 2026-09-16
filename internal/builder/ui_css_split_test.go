package builder

// ui_css_split_test.go — ui.css 按控件段切分的注入粒度（审计 UIK-013）。
//
// 三条验收直接来自条目原文：
//  ① 「只用按钮的页面不注入表格与分页的 CSS」；
//  ② 「产物 CSS 体积下降」；
//  ③ 切分不改变语义 —— 全部段命中时与原文件逐字节相同，段标记出错时构建失败
//     而不是静默丢样式。

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// fullUICSS 真实的 ui.css 源（与生产装配同一条读取路径）。
func fullUICSS(t *testing.T) string {
	t.Helper()
	css := templates.UICSS()
	if strings.TrimSpace(css) == "" {
		t.Fatal("ui.css 为空：切分断言会空转")
	}
	if !strings.Contains(css, "/* ===== 按钮忙碌态（js/ui/busy.js）=====") {
		t.Fatal("ui.css 缺少段标记：切分器会退化成「未切分源」，本文件全部断言随之失效")
	}
	return css
}

// TestUICSSSplitReassemblesSource 全部段拼回去必须与原文件逐字节相同。
//
// 这是「切分没有改语义」的最硬证据：段之间是连续切片，任何一段被截断或重复都会现形。
func TestUICSSSplitReassemblesSource(t *testing.T) {
	src := fullUICSS(t)
	chunks, ok, err := splitUICSS(src)
	if err != nil || !ok {
		t.Fatalf("切分失败：ok=%v err=%v", ok, err)
	}
	if len(chunks) != len(uiCSSSections()) {
		t.Fatalf("段数 %d，段表 %d", len(chunks), len(uiCSSSections()))
	}
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(c.text)
	}
	if sb.String() != src {
		t.Fatal("段拼接结果与原文件不一致：切分改动了源文本")
	}
}

// TestUICSSSplitSectionTableMatchesSource 段表与源文件一一对应（标题在、顺序对）。
func TestUICSSSplitSectionTableMatchesSource(t *testing.T) {
	src := fullUICSS(t)
	for _, sec := range uiCSSSections() {
		if sec.anchor == "" {
			continue
		}
		if !strings.Contains(src, sec.anchor) {
			t.Errorf("段 %s 的标题在 ui.css 里找不到：切分会报错（构建失败），请同步段表与源文件", sec.id)
		}
	}
	// 每个段都要有内容：空段说明标题顺序写错了位置（段被下一个标题「吃掉」）。
	chunks, _, err := splitUICSS(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chunks {
		if strings.TrimSpace(c.text) == "" {
			t.Errorf("段 %s 是空的：段表顺序与源文件不符", c.id)
		}
	}
}

// TestUICSSInjectsOnlyUsedSections 只用按钮的页面不注入表格与分页（UIK-013 验收第一条）。
func TestUICSSInjectsOnlyUsedSections(t *testing.T) {
	src := fullUICSS(t)
	css, err := uiCSSFor(src, collectHTMLScan("<button class='btn btn-primary'>提交</button>"))
	if err != nil {
		t.Fatal(err)
	}
	if css == "" {
		t.Fatal("写了 .btn 的页面拿不到按钮段样式")
	}
	for _, want := range []string{".btn-primary", "--ui-sp-sm:"} {
		if !strings.Contains(css, want) {
			t.Errorf("缺少 %q（按钮段或公共令牌段没带上）", want)
		}
	}
	for _, unwanted := range []string{".data-table", ".pagination-btn", ".lang-select", ".wbs-trigger", ".sky-c-"} {
		if strings.Contains(css, unwanted) {
			t.Errorf("页面没用到的段被注入了：出现 %q", unwanted)
		}
	}
	if len(css) >= len(src) {
		t.Errorf("切分后字节数没有下降：%d >= %d", len(css), len(src))
	}
	t.Logf("[UIK-013] 只用按钮：ui.css %d 字节 → 注入 %d 字节（%.0f%%）",
		len(src), len(css), 100*float64(len(css))/float64(len(src)))
}

// TestUICSSSplitUtilityClassesFollowNotTrigger 工具类不触发注入，但跟随已命中的段一起带。
func TestUICSSSplitUtilityClassesFollowNotTrigger(t *testing.T) {
	src := fullUICSS(t)
	onlyUtility, err := uiCSSFor(src, collectHTMLScan("<div class='flex items-center'>布局</div>"))
	if err != nil {
		t.Fatal(err)
	}
	if onlyUtility != "" {
		t.Error("只写工具类不该触发任何注入（工具类几乎每页都有，命中即整份 ui.css）")
	}
	withButton, err := uiCSSFor(src, collectHTMLScan("<div class='flex'><button class='btn'>提交</button></div>"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withButton, ".flex {") {
		t.Error("命中控件段时公共工具类段应一并带上（作者写在同一页的 .flex 不能失效）")
	}
}

// TestUICSSSplitResourceTriggersSection 属性触发资源时带上对应段（class 无关）。
func TestUICSSSplitResourceTriggersSection(t *testing.T) {
	src := fullUICSS(t)
	css, err := uiCSSFor(src, collectHTMLScan("<button data-modal-open='f1'>打开</button>"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(css, ".wb-modal-inner") {
		t.Fatalf("命中 modal.js 却没带上模态段；注入内容：\n%s", css)
	}
	if strings.Contains(css, ".data-table") {
		t.Error("没有用到的表格段被带上")
	}
}

// TestUICSSSplitResourcesCovered 每个控件资源要么映射到段，要么显式说明没有样式段。
func TestUICSSSplitResourcesCovered(t *testing.T) {
	byFile := map[string]bool{}
	for _, sec := range uiCSSSections() {
		for _, f := range sec.files {
			byFile[f] = true
		}
	}
	noStyle := uiCSSSectionResourcesWithoutStyle()
	for _, block := range uiBlocks {
		if byFile[block.file] {
			continue
		}
		reason, ok := noStyle[block.file]
		if !ok {
			t.Errorf("控件资源 %s 既没有对应的样式段、也没有在 uiCSSSectionResourcesWithoutStyle 里说明："+
				"命中它时到底该注入哪一段会变成隐性知识", block.file)
			continue
		}
		if len([]rune(strings.TrimSpace(reason))) < 12 {
			t.Errorf("资源 %s 的「无样式段」说明太短，写清它为什么没有基座外观", block.file)
		}
		if block.noStyle && !strings.Contains(reason, "无样式") && !strings.Contains(reason, "行为") {
			t.Errorf("资源 %s 标了 noStyle 却缺少对应说明", block.file)
		}
	}
}

// TestUIBaseClassesMapToTriggerableSections 基座清单里的类必须落在可触发的段里。
//
// 清单（文档口径）与段表（注入粒度）是两套机制：清单说「这个类该有样式」，
// 段表说「那份样式在哪一段」。类落在公共段（工具类那种）时，写法上「有样式」但
// 永远不会被它触发 —— 症状是「作者按文档写类，却什么都没发生」。
func TestUIBaseClassesMapToTriggerableSections(t *testing.T) {
	src := fullUICSS(t)
	chunks, _, err := splitUICSS(src)
	if err != nil {
		t.Fatal(err)
	}
	triggerable := map[string]bool{}
	for _, c := range chunks {
		sec := uiCSSSectionByID(c.id)
		if sec.trigger != sectionTriggerClasses {
			continue
		}
		for cls := range sectionClasses(c.text) {
			triggerable[cls] = true
		}
	}
	for _, cls := range uiBaseClasses {
		if !triggerable[cls] {
			t.Errorf("基座清单里的类 %q 不在任何可触发段里：作者写它不会触发注入（清单与段表脱钩）", cls)
		}
	}
}
