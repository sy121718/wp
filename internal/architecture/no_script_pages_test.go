package architecture

// no_script_pages_test.go — 「无脚本可用」的那几页不许引入脚本依赖。
//
// ## 判据
//
// 两个页面的失败路径必须在**什么 JS 都没有**的前提下可用：
//
//  1. **访客页**（user/layout.html + 登录 / 注册 / 找回 / 重置 / 账号 / 消息 / 完成页）：
//     layout.html 开头写着「访客侧要求无脚本可用」。注册页尤其不能因为浏览器的
//     脚本设置而打不开 —— 那是新用户唯一的入口。
//  2. **工作台错误壳**（workbench/error.html）：它的存在前提就是「编辑器打不开」，
//     而编辑器自己的脚本可能根本没加载起来。错误页若依赖 htmx，等于在失败时再失败一次。
//
// 所以这两处的 c.HTML 是**终局形态而非欠账**：后台页失败回 JSON（有 toast），
// 这两处必须回可独立渲染的 HTML。
//
// ## 为什么需要这道门禁
//
// 「所有写动作失败都回 JSON」这个收敛方向是对的，但它有一个诱人的副作用：
// 顺手把这两处也统一掉。统一的代价不是风格不一致，而是**注册页在无脚本环境下打不开**、
// **编辑器坏掉时用户看到一段 JSON 文本**。这两个后果都不会让任何测试变红 ——
// 除非有这条断言。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scriptTagRe 匹配模板里的 <script> 标签（含 src / 内联）。
var scriptTagRe = regexp.MustCompile(`(?i)<script\b`)

// noScriptPages 必须在无脚本环境下可用的模板 → 理由。
//
// 按 glob 登记而不是逐个列文件：新增一个访客页（比如「邀请注册」）时，
// 它落在 user/ 下就该自动受这条约束，不需要有人记得来加一行。
var noScriptPages = []struct {
	glob   string
	reason string
}{
	{
		glob:   "internal/templates/user/*.html",
		reason: "访客页要求无脚本可用：注册 / 登录 / 找回是用户入口，不能因浏览器脚本设置打不开",
	},
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
					"  这一页的失败路径必须无脚本可用；需要交互就写在 Go 侧（服务端渲染 + 原生表单）",
					relToRoot(t, root, file), page.reason)
			}
		}
	}
	if checked < 8 {
		t.Fatalf("只检查了 %d 个模板：判据覆盖面变小了（glob 写窄了？）", checked)
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
