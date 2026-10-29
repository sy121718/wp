package architecture

// 工作台错误壳在编辑器脚本未加载时仍须独立显示。
// 访客账号旧整页已退役，账号组件改走静态外壳与JSON数据，不再登记不存在的user模板。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scriptTagRe 匹配模板里的 <script> 标签（含 src / 内联）。
var scriptTagRe = regexp.MustCompile(`(?i)<script\b`)

// noScriptPages 声明必须无脚本可用的现行模板及理由。
// 每一登记都必须命中真实文件；模板删除时同步收口，不能保留失效glob。
var noScriptPages = []struct {
	glob   string
	reason string
}{
	{
		glob:   "internal/templates/workbench/error.html",
		reason: "工作台错误壳的前提就是「编辑器打不开」——它不能依赖编辑器自己的脚本",
	},
}

func TestNoScriptPagesStayScriptFree(t *testing.T) {
	root := repoRoot(t)
	checked := 0
	for _, page := range noScriptPages {
		files, err := filepath.Glob(filepath.Join(root, page.glob))
		if err != nil {
			t.Fatalf("展开 %s 失败: %v", page.glob, err)
		}
		if len(files) == 0 {
			t.Errorf("%s 一个文件都没匹配到：判据失效（模板搬家了？）", page.glob)
			continue
		}
		for _, file := range files {
			body, readErr := os.ReadFile(file)
			if readErr != nil {
				t.Fatalf("读取 %s 失败: %v", file, readErr)
			}
			checked++
			if loc := scriptTagRe.FindStringIndex(string(body)); loc != nil {
				t.Errorf("%s 引入了 <script>（%s）\n"+
					"  这一页的失败路径必须无脚本可用；错误说明和返回链接必须通过静态HTML可用",
					relToRoot(t, root, file), page.reason)
			}
		}
	}
	if checked == 0 {
		t.Fatal("未检查任何无脚本模板，判据失效")
	}
}

// TestNoScriptPagesDoNotUseHtmxAttributes 无脚本页不许挂 htmx 的 hx-* 指令。
//
// 与上一条分开是因为后果不同：`<script>` 是主动引入依赖，`hx-post` 是**悄悄失效**——
// 没有 htmx 时浏览器会退回原生提交，页面看起来能用，但 hx-target / hx-swap 全部不生效，
// 于是「失败只回片段」那种设计会渲染出一个裸片段。
func TestNoScriptPagesDoNotUseHtmxAttributes(t *testing.T) {
	root := repoRoot(t)
	hxAttr := regexp.MustCompile(`(?i)\bhx-(get|post|put|patch|delete|target|swap|include|trigger)\s*=`)
	for _, page := range noScriptPages {
		files, err := filepath.Glob(filepath.Join(root, page.glob))
		if err != nil {
			continue
		}
		for _, file := range files {
			body, readErr := os.ReadFile(file)
			if readErr != nil {
				continue
			}
			for _, m := range hxAttr.FindAllString(string(body), -1) {
				t.Errorf("%s 挂上了 %s（%s）\n  没有 htmx 时这些属性静默失效，退化成原生提交",
					relToRoot(t, root, file), strings.TrimSpace(m), page.reason)
			}
		}
	}
}
