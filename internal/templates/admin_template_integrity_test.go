package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	jetCommentOpen  = "{*"
	jetCommentClose = "*}"
)

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

func yieldTakesContent(tag string) bool {
	if !startsWithTagWord(tag, "yield") {
		return false
	}
	fields := strings.Fields(tag)
	return len(fields) >= 3 && fields[len(fields)-1] == "content"
}

func hasRangeVarT(tag string) bool {
	if !startsWithTagWord(tag, "range") {
		return false
	}
	return strings.Contains(tag, " t :=") || strings.Contains(tag, ",t :=")
}

func templateProblems(src string) []string {
	var problems []string

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

			opens++
		}
		if hasRangeVarT(tag) {
			rangeVarT = true
		}

		if strings.HasPrefix(tag, "t(") || strings.HasPrefix(tag, "t (") {
			varTCall = true
		}

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

	if len(rangeVarT) > 0 {
		t.Logf("以下模板用 t 作 range 变量（合法但会遮蔽翻译函数名，改这些文件时注意）：%s",
			strings.Join(rangeVarT, ", "))
	}
}

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

	good := "{{if .A}}{{ .[t](k, 兜底) }}{{range _, tag := .Tags}}{{tag}} {{end}}{{end}}"
	if problems := templateProblems(good); len(problems) != 0 {
		t.Errorf("合法模板被判成有问题：%v（样本 %s）", problems, good)
	}
}
