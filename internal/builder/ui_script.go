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

func usedUIFiles(content string) []string {
	hits := make([]bool, len(uiBlocks))
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break // 输入是已编译的 HTML 字符串，读取到 EOF 即结束。
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		_, more := z.TagName()
		for more {
			var key []byte
			key, _, more = z.TagAttr()
			for i, block := range uiBlocks {
				if hits[i] {
					continue
				}
				for _, attr := range block.attrs {
					if string(key) == attr {
						hits[i] = true
						break
					}
				}
			}
		}
	}
	var files []string
	for i, block := range uiBlocks {
		if hits[i] {
			files = append(files, block.file)
		}
	}
	return files
}

// uiAssetsFor 一次识别能力并同时组装 CSS/JS，防止两次扫描的规则漂移。
// sources=nil 表示调用方选择无脚本输出；非 nil（含空 map）表示已启用控件增强，
// 命中的控件、基座、入口或样式缺失都返回构建错误，不能生成残缺产物。
func uiAssetsFor(content, css string, sources map[string]string) (string, string, error) {
	files := usedUIFiles(content)
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
