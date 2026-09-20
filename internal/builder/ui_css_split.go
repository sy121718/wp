package builder

// ui_css_split.go — ui.css 按控件段切分，注入时只带用到的段（审计 UIK-013）。
//
// 为什么切分：ui.css 是后台（<link> 整份）与产物（构建期内联）共用的一份文件。
// 后台长驻、可以整份加载；产物是静态发布，命中一个控件类就内联**整份 737 行**，
// 只用按钮的页面白带表格、分页、语言切换那一堆规则（UIK-013 的原始症状）。
// 切分之后注入粒度 = 用到的控件段，字节随用量走。
//
// 为什么不把 ui.css 拆成多个文件：拆文件会让「一份真源」变成多份，后台的 <link>
// 也要跟着改成多份或做打包。这里保持单文件不变，切分只在构建期发生（后台那条路径
// 完全不受影响），而且段与段之间是**连续切片**——全部命中时拼回去与原文件逐字节相同
// （TestUICSSSplitReassemblesSource 直接断言这一点）。
//
// 消费端归属（审计 UI-01）：段表回答「这段样式在哪一段」，owner 回答「这段投递给谁」。
// ui.css 是**一份源两种投递**（后台 <link> 整份、产物按段内联），而后台宿主类
// （.admin-layout）与后台外壳控件（.lang-switch / .lang-select）与控件基座住在同一个文件里 ——
// 它们的触发类一旦被作者写进产物页面，就会把后台专用规则带进静态产物。
// owner=ownerBackend 的段在产物注入时**整段跳过**（既不算命中、也不输出）。
// 后台与工作台那条投递路径不受影响：它们是 <link href="/static/css/ui.css"> 直引整份文件
// （admin/layout.html:23-25、workbench/layout.html:12-16），根本不经过这里的段切分。
//
// 段怎么定位：按段表登记的**标题行**（去空白后整行相等）顺序查找，顺序必须单调。
// 标题被改掉时应当「构建失败」，而不是「每页悄悄少一段样式」—— 所以段标记不完整
// 一律报错。源里一个标题都没有（测试桩、插件自带样式）时视为「未切分源」，原样返回。
//
// 触发策略两种：
//   · sectionTriggerClasses —— 段里定义了哪些类，页面写了哪个就带上这一段
//     （**自动推导**：段内新增类无需改表，类在段里定义、页面写了就命中，两边永远同步）；
//   · sectionTriggerPublic  —— 公共段（令牌、工具类、文件头说明）：自己不触发，
//     但只要有任何一段被命中就一并带上。工具类不能单独触发注入 —— 那等于每页整份。
//
// 与 uiBaseClasses 的关系：那份清单是**文档口径**（作者会手写的控件外观类，有对表测试
// 盯着），段切分是**注入粒度**。两者描述同一件事的两面：清单说「这个类该有样式」，
// 段表说「那份样式在哪一段」。清单里的类必须落在某个可触发段里 ——
// TestUIBaseClassesMapToTriggerableSections 断言这一点。

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// 触发策略。
const (
	sectionTriggerClasses = iota + 1 // 段内类名直接触发
	sectionTriggerPublic             // 公共段：随其它段一起带
)

// 消费端归属（审计 UI-01）。零值即 ownerShared —— 段表不写 owner 就是两端都投递。
type uiCSSOwner int

const (
	// ownerShared 后台与产物都消费（控件基座、公共令牌、工具类）。
	ownerShared uiCSSOwner = iota
	// ownerBackend 只服务后台 / 工作台（它们直引整份 ui.css），产物注入时整段跳过。
	ownerBackend
	// ownerProduct 只服务站点产物（当前没有这样的段，留位以表达归属维度）。
	ownerProduct
)

// uiCSSSection 一个可独立注入的样式段。
type uiCSSSection struct {
	// id 段标识（日志、测试与诊断用）。
	id string
	// anchor 段起始行的完整内容（去空白后比较）；首段为 ""，表示从文件头开始。
	anchor string
	// trigger 触发策略。
	trigger int
	// owner 消费端归属（见文件头）：ownerBackend 的段不进产物。
	owner uiCSSOwner
	// files 命中这些控件资源（uiBlocks 的 file）时带上本段。
	files []string
}

// uiCSSSections 段表（顺序即 ui.css 里的顺序，切分与拼接都按它走）。
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
		{id: "base-doc", anchor: "原始控件（与 js/ui/*.js 配套）—— 后台、工作台、前台产物共用一份", trigger: sectionTriggerPublic},
		{id: "tokens", anchor: "/* ===== 尺寸与动效的基础量（theme.css 缺失时的兜底）=====", trigger: sectionTriggerPublic},
		{id: "buttons", anchor: "按钮", trigger: sectionTriggerClasses},
		{id: "cards", anchor: "卡片", trigger: sectionTriggerClasses},
		{id: "tables", anchor: "表格", trigger: sectionTriggerClasses},
		{id: "forms", anchor: "表单", trigger: sectionTriggerClasses},
		// 后台裸控件的兜底外观（UIK-009）：宿主类 .admin-layout 只出现在 admin/layout.html，
		// 站点产物侧没有任何消费者 —— 标 backend 后它不再随 forms 段进产物（UI-01）。
		{id: "bareform", anchor: "/* ===== 后台裸控件的兜底外观（审计 UIK-009）=====", trigger: sectionTriggerClasses, owner: ownerBackend},
		{id: "badges", anchor: "徽章 / 标签 / 状态", trigger: sectionTriggerClasses},
		{id: "pagination", anchor: "分页", trigger: sectionTriggerClasses},
		{id: "utilities", anchor: "工具类", trigger: sectionTriggerPublic},
		{id: "themetoggle", anchor: "主题切换按钮", trigger: sectionTriggerClasses, files: []string{"themetoggle.js"}},
		// 语言切换是后台外壳（admin/layout.html 与 admin/login.html）。段里含通用无障碍类 .sr-only ——
		// 它在产物侧当前没有消费者；将来站点组件要用它，应当把它拆到 shared 段，
		// 而不是解除本段的 backend 归属（门禁见 internal/templates/ui_css_ownership_test.go）。
		{id: "langswitch", anchor: "语言切换（后台外壳，多语言 P1 第二步）：GET /admin/lang 表单，零 JS 依赖", trigger: sectionTriggerClasses, owner: ownerBackend},
		// 页签基座（UIK-011）与语言切换住在同一个文件尾部，但它是**通用内容区基座**：
		// 当前只有后台在用（双向守卫见 ui_css_ownership_test.go），产物侧将来要用就该能注入 ——
		// 所以它必须是独立段、owner=shared，不能跟着 langswitch 一起被判进 backend。
		{id: "tabs", anchor: "页签（tabs）基座 —— UIK-011", trigger: sectionTriggerClasses},
	}
}

// uiCSSSectionResourcesWithoutStyle 有资源、但没有独立样式段的控件文件 → 说明。
//
// 与片段基座的两张表同思路：不让「这个控件的样式在哪一段」变成隐性知识。
// 这里登记的是事实：这些资源要么只驱动行为（没有配套外观），要么外观属于宿主页面。
func uiCSSSectionResourcesWithoutStyle() map[string]string {
	return map[string]string{
		"htmx.min.js":  "行为库：htmx 指令不带来任何基座外观（它的过渡类在 htmx 源码里）",
		"_util.js":     "基座助手：只提供 WBUI 注册表与工具函数，无样式",
		"index.js":     "基座入口：DOM 扫描与 htmx:afterSwap 重扫，无样式",
		"iconfield.js": "图标选择字段的外观属于宿主页面的表单布局（后台外壳的 theme.css），没有独立基座段；产物里没有它的消费方",
	}
}

// uiCSSChunk 切分结果：段标识 + 原文切片。
type uiCSSChunk struct {
	id   string
	text string
}

// splitUICSS 按段表切分源码。
//
// ok=false 表示「源里没有段标题」——调用方应原样使用整份源（测试桩、插件自带样式）。
// 标题只命中一部分属于真源被改坏：返回错误让构建失败。
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
	if found <= 1 { // 只有首段（没有标题）—— 未切分源
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

// indexAnchorLine 从 from 起找到「去空白后以 anchor 开头的整行」，返回该行起始偏移。
func indexAnchorLine(src, anchor string, from int) int {
	for {
		idx := strings.Index(src[from:], anchor)
		if idx < 0 {
			return -1
		}
		at := from + idx
		lineStart := strings.LastIndexByte(src[:at], '\n') + 1
		if strings.TrimSpace(src[lineStart:at]) == "" {
			return lineStart
		}
		from = at + len(anchor)
	}
}

// sectionClasses 段里定义的类名集合（剥注释后按 .class 提取，自动推导触发条件）。
func sectionClasses(text string) htmlFeatures {
	out := htmlFeatures{}
	for _, m := range reSectionClass.FindAllStringSubmatch(maskCSSNonCode(text), -1) {
		out[strings.ToLower(m[1])] = struct{}{}
	}
	return out
}

// reSectionClass 类选择器提取（够用即可：输入是受控语法的 ui.css）。
var reSectionClass = regexp.MustCompile("[.]([a-zA-Z][a-zA-Z0-9_-]*)")

// uiCSSFor 按页面用到的控件选出要注入的段（未命中任何段时返回空串）。
//
// 未切分源（无段标题）原样返回：测试桩与插件样式走这条路径，行为与切分前一致。
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
			continue // 后台专属段既不算命中、也不输出（见文件头的 owner 说明）
		}
		if sec.trigger == sectionTriggerPublic {
			continue // 公共段不参与「有没有命中」判定，见下面的拼接
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
			continue // 后台专属段不进产物
		}
		if sec.trigger == sectionTriggerPublic || hit[c.id] {
			sb.WriteString(c.text)
		}
	}
	return sb.String(), nil
}

// uiCSSSectionByID 按 id 取段定义（段表是常量，找不到属于代码缺陷）。
func uiCSSSectionByID(id string) uiCSSSection {
	for _, sec := range uiCSSSections() {
		if sec.id == id {
			return sec
		}
	}
	panic("ui_css_split: 未知的段 id " + id)
}

// BackendExclusiveSectionClasses 返回**只在后台专属段里定义**的类名（审计 UI-01）。
//
// 用途：产物注入会跳过 owner=backend 的段，所以这些类在站点侧没有样式来源。
// 门禁（internal/templates/ui_css_ownership_test.go）用它断言这些类不出现在
// 后台 / 工作台之外的模板里 —— 哪天有人把 .lang-switch 用到站点组件上，那条测试必须红，
// 逼他把该类提升为 shared 段，而不是让它静默无样式。
//
// 同时出现在别的段里的类不算「后台专属」（例如 .wbs-native 既在 select 段定义、
// 又出现在兜底段的选择器里）。未切分源（测试桩 / 插件样式）返回空集。
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

// secHits 该段是否被页面命中：段内类名出现在页面上，或对应控件资源被用到。
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
