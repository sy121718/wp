package builder

// 控件资源来自公共 UI Kit；后台通过静态路径加载，访问产物只携带实际用到的闭包。

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// 文件与触发属性在这里声明一次；装配层从 UIAssetFiles 获取源码清单。
// 属性按名称精确匹配，不将正文、注释、脚本中的示例当成控件。
type uiBlock struct {
	file  string
	attrs []string
	// prefixes 前缀匹配。htmx 的指令家族（hx-get / hx-post / hx-trigger / hx-target …）
	// 无法穷举：逐条列出的代价是「每个人加一条指令时都得记得回来改这张表」，
	// 漏掉的表现是那条指令在产物里静默失效。
	prefixes []string
	// noStyle 标记该资源不需要配套样式：ui.css 是控件外观，htmx 是行为库。
	noStyle bool
}

// uiBaseClasses 基座样式类名：只写 class 也应注入 ui.css（UIK-002：样式与行为解耦）。
var uiBaseClasses = []string{
	"btn", "btn-primary", "btn-secondary", "btn-ghost", "btn-danger", "btn-sm", "btn-icon",
	"wb-btn", "wb-btn-primary", "wb-btn-secondary", "wb-btn-ghost", "wb-icon-btn",
	"form-input", "form-select", "form-textarea", "form-label", "form-hint", "form-error",
	"wbs", "form-inline", "form-row",
}

var uiBlocks = []uiBlock{
	// htmx 是访问面所有 hx-* 属性的唯一执行者，也是控件入口 index.js 监听
	// htmx:afterSwap 的前提。产物此前从不携带它 —— 组件按 HTMX 约定写出的
	// hx-get / hx-post 在访问面全部是哑属性（组件自己降级成原生表单，功能
	// 「看着能用」但走的不是预想路径，这类静默降级最难发现）。
	{file: "htmx.min.js", prefixes: []string{"hx-"}, noStyle: true},
	{file: "select.js", attrs: []string{"data-ui-select"}},
	{file: "modal.js", attrs: []string{"data-modal", "data-modal-open", "data-modal-close"}},
	{file: "drawer.js", attrs: []string{"data-drawer-open", "data-drawer-close", "data-drawer", "data-drawer-mask"}},
	{file: "confirm.js", attrs: []string{"data-confirm"}},
	{file: "colorfield.js", attrs: []string{"data-color-field"}},
	{file: "iconfield.js", attrs: []string{"data-icon-field", "data-icon-name"}},
	{file: "themetoggle.js", attrs: []string{"data-theme-toggle"}},
}

// UIAssetFiles 是访问产物可用的公共控件资源清单，顺序为助手、控件、扫描入口。
// 每次返回独立切片，调用方不能修改编译器的注册表。
func UIAssetFiles() []string {
	files := make([]string, 0, len(uiBlocks)+2)
	files = append(files, "_util.js")
	for _, block := range uiBlocks {
		files = append(files, block.file)
	}
	return append(files, "index.js")
}

// htmlFeatures 是同一份最终 HTML 的能力属性集合，控件与组件增强共享。
type htmlFeatures map[string]struct{}

// htmlScan 是同一份 HTML 的能力扫描结果：属性驱动行为脚本，class 驱动基座样式。
type htmlScan struct {
	attrs   htmlFeatures
	classes htmlFeatures
}

func collectHTMLFeatures(content string) htmlFeatures {
	return collectHTMLScan(content).attrs
}

func collectHTMLScan(content string) htmlScan {
	out := htmlScan{attrs: make(htmlFeatures), classes: make(htmlFeatures)}
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break // 内存字符串读取到 EOF；脚本、样式和原始文本不当成标签。
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		_, more := z.TagName()
		for more {
			var key, val []byte
			key, val, more = z.TagAttr()
			name := strings.ToLower(string(key))
			if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "hx-") {
				out.attrs[name] = struct{}{}
			}
			if name == "class" {
				for _, token := range strings.Fields(string(val)) {
					if c := strings.ToLower(strings.TrimSpace(token)); c != "" {
						out.classes[c] = struct{}{}
					}
				}
			}
		}
	}
	return out
}

func hasUIBaseClass(classes htmlFeatures) bool {
	for _, c := range uiBaseClasses {
		if _, ok := classes[c]; ok {
			return true
		}
	}
	return false
}

// hit 判断这个资源是否被页面实际用到（精确属性名或前缀任一命中）。
func (b uiBlock) hit(attrs htmlFeatures) bool {
	for _, attr := range b.attrs {
		if _, ok := attrs[attr]; ok {
			return true
		}
	}
	for _, prefix := range b.prefixes {
		for name := range attrs {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}

// usedUIFiles 返回命中的资源与「是否需要控件基座」。
//
// 基座（_util.js 助手 + index.js 扫描入口 + ui.css 样式）只服务控件类资源：
// 一个只用 htmx 属性的页面不该因此带上整套控件基座与样式。
func usedUIFiles(attrs htmlFeatures) (files []string, needBase bool) {
	for _, block := range uiBlocks {
		if !block.hit(attrs) {
			continue
		}
		files = append(files, block.file)
		if !block.noStyle {
			needBase = true
		}
	}
	return files, needBase
}

// uiAssetsFor 一次识别能力并同时组装 CSS/JS，防止两次扫描的规则漂移。
// sources=nil 表示调用方选择无脚本输出；非 nil（含空 map）表示已启用控件增强，
// 命中的控件、基座、入口或样式缺失都返回构建错误，不能生成残缺产物。
func uiAssetsFor(attrs htmlFeatures, css string, sources map[string]string) (string, string, error) {
	return uiAssetsForScan(htmlScan{attrs: attrs}, css, sources)
}

func uiAssetsForScan(scan htmlScan, css string, sources map[string]string) (string, string, error) {
	files, needStyleFromAttrs := usedUIFiles(scan.attrs)
	needStyle := needStyleFromAttrs || hasUIBaseClass(scan.classes)
	if len(files) == 0 && !needStyle {
		return "", "", nil
	}
	if sources == nil {
		if needStyle {
			return css, "", nil
		}
		return "", "", nil
	}
	if len(files) > 0 {
		if needStyleFromAttrs {
			// 基座助手在控件之前（控件靠它注册），入口在最后（它负责扫描已注册的控件）。
			files = append(append([]string{"_util.js"}, files...), "index.js")
		}
	}
	parts := make([]string, 0, len(files))
	for _, file := range files {
		src := sources[file]
		if strings.TrimSpace(src) == "" {
			return "", "", fmt.Errorf("控件资源缺失: %s", file)
		}
		parts = append(parts, src)
	}
	if needStyle && strings.TrimSpace(css) == "" {
		return "", "", fmt.Errorf("控件资源缺失: ui.css")
	}
	outCSS := ""
	if needStyle {
		outCSS = css
	}
	return outCSS, strings.Join(parts, "\n"), nil
}
