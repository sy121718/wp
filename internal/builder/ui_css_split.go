package builder

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	sectionTriggerClasses = iota + 1
	sectionTriggerPublic
)

type uiCSSOwner int

const (
	ownerShared uiCSSOwner = iota

	ownerBackend

	ownerProduct
)

type uiCSSSection struct {
	id string

	anchor string

	trigger int

	owner uiCSSOwner

	files []string
}

func uiCSSSections() []uiCSSSection {
	return []uiCSSSection{
		{id: "head", anchor: "", trigger: sectionTriggerPublic},
		{id: "select", anchor: "/* ===== 自绘下拉（js/ui/select.js）=====", trigger: sectionTriggerClasses, files: []string{"select.js"}},
		{id: "confirm", anchor: "/* ===== 确认框（js/ui/confirm.js）=====", trigger: sectionTriggerClasses, files: []string{"confirm.js"}},
		{id: "modal", anchor: "/* ===== 模态弹窗（js/ui/modal.js）=====", trigger: sectionTriggerClasses, files: []string{"modal.js"}},
		{id: "toast", anchor: "/* ===== 轻提示（js/ui/toast.js）=====", trigger: sectionTriggerClasses},
		{id: "drawer", anchor: "/* ===== 抽屉（js/ui/drawer.js）=====", trigger: sectionTriggerClasses, files: []string{"drawer.js"}},
		{id: "colorfield", anchor: "/* ===== 颜色字段（js/ui/colorfield.js）=====", trigger: sectionTriggerClasses, files: []string{"colorfield.js"}},
		{id: "busy", anchor: "/* ===== 按钮忙碌态（js/ui/busy.js）=====", trigger: sectionTriggerClasses},

		{id: "base-doc", anchor: "/* ===== 原始控件基座（js/ui/*.js）=====", trigger: sectionTriggerPublic},
		{id: "tokens", anchor: "/* ===== 尺寸与动效的基础量（theme.css 缺失时的兜底）=====", trigger: sectionTriggerPublic},
		{id: "buttons", anchor: "按钮", trigger: sectionTriggerClasses},
		{id: "cards", anchor: "卡片", trigger: sectionTriggerClasses},
		{id: "tables", anchor: "表格", trigger: sectionTriggerClasses},

		{id: "forms", anchor: "表单", trigger: sectionTriggerClasses, files: []string{"select.js"}},

		{id: "badges", anchor: "徽章 / 标签 / 状态", trigger: sectionTriggerClasses},
		{id: "pagination", anchor: "分页", trigger: sectionTriggerClasses},
		{id: "utilities", anchor: "工具类", trigger: sectionTriggerPublic},
		{id: "themetoggle", anchor: "主题切换按钮", trigger: sectionTriggerClasses, files: []string{"themetoggle.js"}},

		{id: "langswitch", anchor: "语言切换（后台外壳，多语言 P1 第二步）：GET /admin/lang 表单，零 JS 依赖", trigger: sectionTriggerClasses, owner: ownerBackend},

		{id: "tabs", anchor: "页签（tabs）基座 —— UIK-011", trigger: sectionTriggerClasses},
	}
}

func uiCSSSectionResourcesWithoutStyle() map[string]string {
	return map[string]string{
		"site-data.js":     "公开数据读取的行为基座，无样式，外观由宿主页面提供",
		"site-preview.js":  "统一受控模拟预览状态，只有行为，无独立样式",
		"site-actions.js":  "解析后组件动作的行为分发，无样式，外观归宿主组件",
		"site-orders.js":   "订单售后JSON视图行为，外观由订单组件静态CSS提供",
		"requires_js.js":   "放行依赖脚本的提交按钮，只有行为，无样式",
		"write-base.js":    "统一写请求的行为基座，无样式，展示外观由宿主提供",
		"site-commerce.js": "消费购物车事实的行为脚本，外观由静态壳与组件样式提供",
		"site-user.js":     "消费账号事实与 JSON 写响应的行为脚本，外观由静态账号表单提供",
		"site-catalog.js":  "列表与搜索JSON视图行为，外观由组件静态CSS提供",
		"site-product.js":  "商品事实展示的行为脚本，无样式，外观归宿主组件",
		"htmx.min.js":      "行为库：htmx 指令不带来任何基座外观（它的过渡类在 htmx 源码里）",
		"_util.js":         "基座助手：只提供 WBUI 注册表与工具函数，无样式",
		"index.js":         "基座入口：DOM 扫描与 htmx:after:swap 重扫，无样式",
		"iconfield.js":     "图标选择字段的外观属于宿主页面的表单布局（后台外壳的 theme.css），没有独立基座段；产物里没有它的消费方",
	}
}

type uiCSSChunk struct {
	id   string
	text string
}

func splitUICSS(src string) (chunks []uiCSSChunk, ok bool, err error) {
	sections := uiCSSSections()
	starts := make([]int, len(sections))
	missing := make([]string, len(sections))
	found := 0
	from := 0
	for i, sec := range sections {
		if sec.anchor == "" {
			starts[i] = 0
			found++
			continue
		}
		idx := indexAnchorLine(src, sec.anchor, from)
		if idx < 0 {
			missing[i] = sec.id
			continue
		}
		starts[i] = idx
		from = idx + len(sec.anchor)
		found++
	}
	if found <= 1 {
		return nil, false, nil
	}
	if found != len(sections) {
		var gone []string
		for _, id := range missing {
			if id != "" {
				gone = append(gone, id)
			}
		}
		return nil, false, fmt.Errorf("ui.css 段标记不完整，缺少段 %v：标题被改动后切分会静默丢掉样式，请同步 uiCSSSections", gone)
	}
	for i, sec := range sections {
		end := len(src)
		if i+1 < len(sections) {
			end = starts[i+1]
		}
		if starts[i] > end {
			return nil, false, fmt.Errorf("ui.css 段 %s 的标记顺序与段表不一致（切分会串段）", sec.id)
		}
		chunks = append(chunks, uiCSSChunk{id: sec.id, text: src[starts[i]:end]})
	}
	return chunks, true, nil
}

func indexAnchorLine(src, anchor string, from int) int {
	for {
		idx := strings.Index(src[from:], anchor)
		if idx < 0 {
			return -1
		}
		at := from + idx
		lineStart := strings.LastIndexByte(src[:at], '\n') + 1
		if strings.TrimSpace(src[lineStart:at]) == "" {
			if open := openCommentStart(src, lineStart); open >= 0 {
				return open
			}
			return lineStart
		}
		from = at + len(anchor)
	}
}

func openCommentStart(src string, pos int) int {
	if !inOpenCSSComment(src[:pos]) {
		return -1
	}

	i := strings.LastIndex(src[:pos], "/*")
	if i < 0 {
		return -1
	}
	return strings.LastIndexByte(src[:i], '\n') + 1
}

func sectionClasses(text string) htmlFeatures {
	out := htmlFeatures{}
	for _, m := range reSectionClass.FindAllStringSubmatch(maskCSSNonCode(text), -1) {
		out[strings.ToLower(m[1])] = struct{}{}
	}
	return out
}

var reSectionClass = regexp.MustCompile("[.]([a-zA-Z][a-zA-Z0-9_-]*)")

func uiCSSFor(css string, scan htmlScan) (string, error) {
	if strings.TrimSpace(css) == "" {
		return "", nil
	}
	chunks, ok, err := splitUICSS(css)
	if err != nil {
		return "", err
	}
	if !ok {
		return css, nil
	}
	usedFiles := map[string]bool{}
	for _, block := range uiBlocks {
		if block.hit(scan.attrs) {
			usedFiles[block.file] = true
		}
	}
	hit := map[string]bool{}
	matched := false
	for _, c := range chunks {
		sec := uiCSSSectionByID(c.id)
		if sec.owner == ownerBackend {
			continue
		}
		if sec.trigger == sectionTriggerPublic {
			continue
		}
		if secHits(sec, c.text, scan, usedFiles) {
			hit[c.id] = true
			matched = true
		}
	}
	if !matched {
		return "", nil
	}
	var sb strings.Builder
	for _, c := range chunks {
		sec := uiCSSSectionByID(c.id)
		if sec.owner == ownerBackend {
			continue
		}
		if sec.trigger == sectionTriggerPublic || hit[c.id] {
			sb.WriteString(repairCommentBoundaries(c.text))
		}
	}
	return sb.String(), nil
}

func repairCommentBoundaries(text string) string {
	if text == "" {
		return text
	}
	const (
		openMark  = "/*"
		closeMark = "*/"
	)
	var b strings.Builder
	if strings.HasPrefix(strings.TrimSpace(text), closeMark) {
		b.WriteString(openMark + "\n")
	}
	b.WriteString(text)
	if inOpenCSSComment(text) {
		b.WriteString("\n" + closeMark + "\n")
	}
	return b.String()
}

func inOpenCSSComment(s string) bool {
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return true
			}
			i += 2 + end + 1
		}
	}
	return false
}

func uiCSSSectionByID(id string) uiCSSSection {
	for _, sec := range uiCSSSections() {
		if sec.id == id {
			return sec
		}
	}
	panic("ui_css_split: 未知的段 id " + id)
}

func BackendExclusiveSectionClasses(css string) ([]string, error) {
	if strings.TrimSpace(css) == "" {
		return nil, nil
	}
	chunks, ok, err := splitUICSS(css)
	if err != nil || !ok {
		return nil, err
	}
	backend, other := map[string]bool{}, map[string]bool{}
	for _, c := range chunks {
		isBackend := uiCSSSectionByID(c.id).owner == ownerBackend
		for cls := range sectionClasses(c.text) {
			if isBackend {
				backend[cls] = true
			} else {
				other[cls] = true
			}
		}
	}
	out := make([]string, 0, len(backend))
	for cls := range backend {
		if other[cls] {
			continue
		}
		out = append(out, cls)
	}
	sort.Strings(out)
	return out, nil
}

func secHits(sec uiCSSSection, text string, scan htmlScan, usedFiles map[string]bool) bool {
	for _, f := range sec.files {
		if usedFiles[f] {
			return true
		}
	}
	for cls := range sectionClasses(text) {
		if _, ok := scan.classes[cls]; ok {
			return true
		}
	}
	return false
}
