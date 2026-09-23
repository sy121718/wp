package templates

// admin_template_integrity_test.go — 模板的结构完整性检查（整页级事故的持续判据）。
//
// 为什么需要它：本项目出过一类事故，不是逻辑错误、也不会让既有测试变红，用户看到的是
// 「页面白屏」或「页面从中间起整块消失」，而 HTTP 状态码始终是 200：
//   · content_templates.html 里的 Jet 注释没闭合 —— 后半张页面被整块吞掉；
//   · 后台模板全面 t() 化之后，请求数据里没注入翻译函数 —— Jet 在第一次取词处中断渲染，
//     body 从那行起整块消失。
//
// 这类问题靠人眼看截图发现的成本极高（要打开那个页面，还得知道它本来该长什么样），所以这里
// 用三条不需要数据库、也不需要渲染的机械判据钉住，对 admin / fragments / site 下的全部模板
// 生效 —— 它们是整页级改动的回归网：
//
//  1. 控制块配平：if / range / block / with 与 end 的数量一致；
//  2. Jet 注释配平：左花括号星号 与 星号右花括号 的数量一致（不配平会吞掉后续内容）；
//  3. 取词方式：不得出现变量形式的 t(…) 调用 —— range 变量 t 会遮蔽翻译函数，那会让 Jet 在
//     运行期报错并中断整页（同样是 200 加半截页面）；也不得在没有 range 变量 t 的模板里
//     裸输出 t（输出的会是函数值，不是译文）。
//
// 统计一律在剥掉注释之后进行：注释里常写示例代码（sidebar.html 有 if 示例、login.html 有
// DevLogin 示例），照字面统计会误报。扫描器是手写的字符遍历而不是正则 —— 注释与模板标记里
// 全是花括号与反斜杠，正则版本可读性差且容易被转义问题误导。
//
// 判据是机械的：它不关心文案对错，只关心结构还完不完整。判据自身的有效性由
// TestTemplateIntegrityChecksAreNotVacuous 用人造坏样本钉住。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jetCommentOpen / jetCommentClose：Jet 注释定界符（单花括号）。
const (
	jetCommentOpen  = "{*"
	jetCommentClose = "*}"
)

// stripJetComments 去掉 Jet 注释，返回剥离后的文本。注释未闭合时返回其之前的内容
// （未闭合意味着后面全被吞掉，调用方会通过配平判据报出来）。
func stripJetComments(src string) string {
	var b strings.Builder
	for {
		i := strings.Index(src, jetCommentOpen)
		if i < 0 {
			b.WriteString(src)
			return b.String()
		}
		b.WriteString(src[:i])
		rest := src[i+len(jetCommentOpen):]
		j := strings.Index(rest, jetCommentClose)
		if j < 0 {
			return b.String()
		}
		src = rest[j+len(jetCommentClose):]
	}
}

// scanTemplateTags 依次吐出每个模板标记里的内容（两个花括号之间的部分，已 TrimSpace）。
func scanTemplateTags(src string) []string {
	var tags []string
	for {
		i := strings.Index(src, "{{")
		if i < 0 {
			return tags
		}
		rest := src[i+2:]
		j := strings.Index(rest, "}}")
		if j < 0 {
			return tags
		}
		tags = append(tags, strings.TrimSpace(rest[:j]))
		src = rest[j+2:]
	}
}

// startsWithTagWord 判断标记内容是否以某个关键字开头，且后面跟的是分隔符（空白或左括号）。
// 这样 {{else if …}} 不会被当成新的 if —— 它属于已经计过数的那个 if。
func startsWithTagWord(tag, word string) bool {
	if !strings.HasPrefix(tag, word) {
		return false
	}
	if len(tag) == len(word) {
		return true
	}
	switch tag[len(word)] {
	case ' ', '	', '(':
		return true
	}
	return false
}

// yieldTakesContent 判断 yield 是否为「内容槽」形式（{{yield b(args) content}} … {{end}}）。
//
// Jet 的 yield 有两种形态，配平要求相反：
//   - 无内容：{{yield b(args)}}          —— 不需要 end；
//   - 带内容：{{yield b(args) content}}  —— 语法上必须配一个 end。
//
// 只把后者计入 opens。漏掉这条判据会让内容槽形式被误判成「end 多 1」：本项目抽
// bulk-bar 片段时用过内容槽写法，16 个模板同时报错，而模板本身是完全合法的 Jet。
// 反过来若把无内容的 yield 也计入，则每个正常调用都会报「缺 end」。
//
// 判据用「最后一个词恰好是 content」而不是 HasSuffix("content")：
// {{yield b mycontent}} 的末词是 mycontent，不该被当成内容槽。
func yieldTakesContent(tag string) bool {
	if !startsWithTagWord(tag, "yield") {
		return false
	}
	fields := strings.Fields(tag)
	return len(fields) >= 3 && fields[len(fields)-1] == "content"
}

// hasRangeVarT 判断标记是不是一个以 t 作循环变量的 range（range _, t := …）。
func hasRangeVarT(tag string) bool {
	if !startsWithTagWord(tag, "range") {
		return false
	}
	return strings.Contains(tag, " t :=") || strings.Contains(tag, ",t :=")
}

// templateProblems 返回一份模板源码的结构问题清单（空表示结构完整）。
func templateProblems(src string) []string {
	var problems []string

	// 先做注释配平：下面的检查都要在剥掉注释之后的文本上做。
	openC, closeC := strings.Count(src, jetCommentOpen), strings.Count(src, jetCommentClose)
	if openC != closeC {
		problems = append(problems, fmt.Sprintf(
			"Jet 注释不配平：%s 共 %d 个，%s 共 %d 个（未闭合的注释会把后续内容整块吞掉，页面从那里起消失）",
			jetCommentOpen, openC, jetCommentClose, closeC))
	}
	stripped := stripJetComments(src)

	var opens, ends int
	var varTCall, bareT, rangeVarT bool
	for _, tag := range scanTemplateTags(stripped) {
		switch {
		case tag == "end":
			ends++
		case startsWithTagWord(tag, "if"), startsWithTagWord(tag, "range"),
			startsWithTagWord(tag, "block"), startsWithTagWord(tag, "with"):
			opens++
		case yieldTakesContent(tag):
			// yield 的「内容槽」形式同样开一个必须闭合的块，见 yieldTakesContent。
			opens++
		}
		if hasRangeVarT(tag) {
			rangeVarT = true
		}
		// 变量形式取词：标记以 t 开头且紧跟左括号（{{t(…)}}）。
		if strings.HasPrefix(tag, "t(") || strings.HasPrefix(tag, "t (") {
			varTCall = true
		}
		// 裸输出：标记内容正好是 t。
		if tag == "t" {
			bareT = true
		}
	}
	if opens != ends {
		problems = append(problems, fmt.Sprintf(
			"控制块不配平：if/range/block/with 共 %d 个，end 共 %d 个（那一行之后的内容会被整块吞掉或渲染中断，而 HTTP 仍是 200）",
			opens, ends))
	}
	if varTCall {
		scope := "不存在"
		if rangeVarT {
			scope = "存在"
		}
		problems = append(problems, fmt.Sprintf(
			"出现变量形式的翻译调用 t(…)（本文件存在 range 变量 t：%s）：range 变量会遮蔽数据里的翻译函数，"+
				"Jet 会在运行期报错并中断整页。后台模板的取词写法是 map 索引加 key 与兜底文案", scope))
	}
	if bareT && !rangeVarT {
		problems = append(problems, "裸输出 t 但没有 range 变量 t：输出的会是函数值（页面上一串无意义的地址），"+
			"取译文要用 map 索引形式取 t 再调用")
	}
	return problems
}

// adminTemplateFiles 递归列出 admin/ 下的全部模板（含各级子目录与 partials/）。
//
// **不要退回 `filepath.Glob("admin/*.html")`**：模板已按后端模块分进子目录（product/ order/
// system/ …），那个模式只会匹配到根下 3 个壳页面 —— 门禁会静默缩水成「只检查 3 个文件」，
// 它仍然绿，但不再守任何东西（比变红危险得多）。
func adminTemplateFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("遍历 %s 失败: %v", dir, err)
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				walk(p)
				continue
			}
			if strings.HasSuffix(p, ".html") {
				files = append(files, p)
			}
		}
	}
	walk("admin")
	return files
}

// TestTemplatesAreStructurallyIntact 全量模板的结构配平与取词方式检查。
func TestTemplatesAreStructurallyIntact(t *testing.T) {
	files := adminTemplateFiles(t)
	for _, pattern := range []string{
		filepath.Join("fragments", "*.html"),
		filepath.Join("fragments", "*.jet"),
		filepath.Join("site", "*.html"),
		filepath.Join("site", "*.jet"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("模板路径匹配失败（%s）: %v", pattern, err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatal("没有匹配到任何模板文件，路径写错了（测试会变成空转）")
	}

	var rangeVarT []string
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取模板失败 %s: %v", path, err)
		}
		src := string(raw)
		for _, problem := range templateProblems(src) {
			t.Errorf("%s %s", path, problem)
		}
		for _, tag := range scanTemplateTags(stripJetComments(src)) {
			if hasRangeVarT(tag) {
				rangeVarT = append(rangeVarT, path)
				break
			}
		}
	}
	t.Logf("已检查 %d 个模板", len(files))
	// range 变量名 t 本身合法（它只是标签字符串），目前有若干模板这么写；
	// 记下来是为了让下一个改这些模板的人知道这里的 t 不是翻译函数。
	if len(rangeVarT) > 0 {
		t.Logf("以下模板用 t 作 range 变量（合法但会遮蔽翻译函数名，改这些文件时注意）：%s",
			strings.Join(rangeVarT, ", "))
	}
}

// TestTemplateIntegrityChecksAreNotVacuous 判据自检：人造坏模板必须被逐条抓到。
//
// 没有这条，上面那个测试会在判据被改坏之后依然全绿 —— 那正是它要防的那类静默失效。
func TestTemplateIntegrityChecksAreNotVacuous(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"注释未闭合", "{{if .A}}x{* 注释没有结束 {{if .B}}{{end}}"},
		{"控制块缺 end", "{{if .A}}x{{range _, tag := .Tags}}{{tag}}"},
		{"变量形式取词", "{{range _, t := .Tags}}{{t}} {{end}}{{t(k, f)}}"},
		{"无 range 变量却裸输出 t", "{{if .A}}{{t}}{{end}}"},
	}
	for _, tc := range cases {
		if len(templateProblems(tc.src)) == 0 {
			t.Errorf("%s：判据没有抓到人造坏模板 %q —— 这条检查已经失效了", tc.name, tc.src)
		}
	}
	// 合法样本必须无问题（否则判据只会制造噪音，没人会继续看它）。
	good := "{{if .A}}{{ .[t](k, 兜底) }}{{range _, tag := .Tags}}{{tag}} {{end}}{{end}}"
	if problems := templateProblems(good); len(problems) != 0 {
		t.Errorf("合法模板被判成有问题：%v（样本 %s）", problems, good)
	}
}
