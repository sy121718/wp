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
}

var uiBlocks = []uiBlock{
	{file: "select.js", attrs: []string{"data-ui-select"}},
	{file: "modal.js", attrs: []string{"data-modal", "data-modal-open", "data-modal-close"}},
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

func collectHTMLFeatures(content string) htmlFeatures {
	attrs := make(htmlFeatures)
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
			var key []byte
			key, _, more = z.TagAttr()
			if strings.HasPrefix(string(key), "data-") {
				attrs[string(key)] = struct{}{}
			}
		}
	}
	return attrs
}

func usedUIFiles(attrs htmlFeatures) []string {
	var files []string
	for _, block := range uiBlocks {
		for _, attr := range block.attrs {
			if _, ok := attrs[attr]; ok {
				files = append(files, block.file)
				break
			}
		}
	}
	return files
}

// uiAssetsFor 一次识别能力并同时组装 CSS/JS，防止两次扫描的规则漂移。
// sources=nil 表示调用方选择无脚本输出；非 nil（含空 map）表示已启用控件增强，
// 命中的控件、基座、入口或样式缺失都返回构建错误，不能生成残缺产物。
func uiAssetsFor(attrs htmlFeatures, css string, sources map[string]string) (string, string, error) {
	files := usedUIFiles(attrs)
	if len(files) == 0 {
		return "", "", nil
	}
	if sources == nil {
		return css, "", nil
	}
	files = append(append([]string{"_util.js"}, files...), "index.js")
	parts := make([]string, 0, len(files))
	for _, file := range files {
		src := sources[file]
		if strings.TrimSpace(src) == "" {
			return "", "", fmt.Errorf("控件资源缺失: %s", file)
		}
		parts = append(parts, src)
	}
	if strings.TrimSpace(css) == "" {
		return "", "", fmt.Errorf("控件资源缺失: ui.css")
	}
	// document.jet 提供外层 script 标签；此处只输出正文，顺序与注册表一致。
	return css, strings.Join(parts, "\n"), nil
}
