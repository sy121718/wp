package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var scriptTagRe = regexp.MustCompile(`(?i)<script\b`)

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
