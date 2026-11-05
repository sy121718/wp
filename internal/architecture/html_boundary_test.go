package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var htmlLiteralBudget = map[string]int{

	"internal/module/mail/enums/mail_ui_labels.go":  1,
	"internal/module/mail/service/mail_tracking.go": 2,

	"internal/module/mail/inbound/http/mail_page.go": 10,

	"internal/module/workbench/service/workbench.go": 31,

	"internal/module/workbench/inbound/http/canvas_bridge_handle.go": 3,
}

var htmlLiteralRe = regexp.MustCompile(
	"<[ \t]*[/]?[ \t]*(?i:doctype|html|head|body|div|span|table|thead|tbody|tr|td|th|ul|ol|li|p|a|h1|h2|h3|h4|h5|h6|form|input|button|select|option|textarea|label|img|script|style|svg|section|header|footer|nav|template|meta|br|hr|strong|em)[ \t\n>/]")

func TestNoHTMLLiteralsInBackend(t *testing.T) {
	root := repoRoot(t)
	got := map[string]int{}
	for _, dir := range []string{"internal/module", "internal/routers"} {
		walkHTMLLiterals(t, filepath.Join(root, dir), got)
	}

	for file, want := range htmlLiteralBudget {
		if got[file] != want {
			t.Errorf("%s：登记 %d 处 HTML 字面量，实测 %d 处。拼 HTML 请搬进 internal/templates 的 Jet 模板；"+
				"确实还要留在 Go 里就在 htmlLiteralBudget 写明理由并改数字（清零后删条目）", file, want, got[file])
		}
		delete(got, file)
	}
	if len(got) == 0 {
		return
	}
	files := make([]string, 0, len(got))
	for file := range got {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		t.Errorf("%s：Go 代码里出现 %d 处 HTML 标签 —— 后端不拼 HTML，搬进 internal/templates 的模板", file, got[file])
	}
}

func walkHTMLLiterals(t *testing.T, dir string, got map[string]int) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("解析 %s 失败：%v", path, perr)
		}
		count := 0
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			raw, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				raw = lit.Value
			}
			if htmlLiteralRe.MatchString(raw) {
				count++
			}
			return true
		})
		if count > 0 {
			rel, rerr := filepath.Rel(repoRoot(t), path)
			if rerr != nil {
				rel = path
			}
			got[filepath.ToSlash(rel)] += count
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败：%v", dir, err)
	}
}
