package feature

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	mailGuardTemplateDir = "../../../../internal/templates/admin/mail"

	mailGuardRetiredTemplate = "mail_marketing.html"
)

var (
	mailGuardRefRe = regexp.MustCompile(`\{\{\s*(?:extends|include|import)\s+"([^"]+)"`)

	mailGuardCommentRe = regexp.MustCompile(`(?s)\{\*.*?\*\}`)

	mailGuardH1Re = regexp.MustCompile(`<h1[\s>]`)

	mailGuardTheadRe = regexp.MustCompile(`(?s)<thead>.*?</thead>`)

	mailGuardThRe = regexp.MustCompile(`<th[\s>]`)

	mailGuardColspanRe = regexp.MustCompile(`colspan="(\d+)"`)
)

func mailGuardStripComments(src string) string {
	return mailGuardCommentRe.ReplaceAllString(src, "")
}

func mailGuardTemplates(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(mailGuardTemplateDir)
	if err != nil {
		t.Fatalf("读取邮件模板目录失败 %s: %v", mailGuardTemplateDir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		files = append(files, mailGuardTemplateDir+"/"+e.Name())
	}
	if len(files) == 0 {
		t.Fatalf("%s 下没有 .html 模板 —— 路径写错了，判据会变成空转", mailGuardTemplateDir)
	}
	return files
}

func mailGuardRefProblems(path string, src string) []string {
	var problems []string
	dir := filepath.Dir(filepath.FromSlash(path))
	for _, m := range mailGuardRefRe.FindAllStringSubmatch(mailGuardStripComments(src), -1) {
		rel := m[1]
		if !strings.HasSuffix(rel, ".html") {
			rel += ".html"
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(target); err != nil {
			problems = append(problems, "引用 "+m[1]+" 的落点不存在："+filepath.ToSlash(target))
		}
	}
	return problems
}

func TestMailGuardTemplateReferencesResolve(t *testing.T) {
	files := mailGuardTemplates(t)
	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := string(raw)
		checked += len(mailGuardRefRe.FindAllStringSubmatch(mailGuardStripComments(src), -1))
		for _, p := range mailGuardRefProblems(f, src) {
			t.Errorf("%s：%s", f, p)
		}
	}
	if checked == 0 {
		t.Fatal("没扫到任何 extends/import/include —— 判据成了空转（写法或路径变了？）")
	}
	t.Logf("检查了 %d 个邮件模板、%d 处引用", len(files), checked)
}

func TestMailGuardReferenceCheckIsNotVacuous(t *testing.T) {

	sample := mailGuardTemplateDir + "/mail.html"

	bad := `{{extends "./layout.html"}}`
	if got := mailGuardRefProblems(sample, bad); len(got) != 1 {
		t.Fatalf("坏样本应报 1 个问题，实际 %d 个：%v", len(got), got)
	}
	good := `{{extends "../layout.html"}}` + "\n" + `{{include "../partials/pagination.html"}}`
	if got := mailGuardRefProblems(sample, good); len(got) != 0 {
		t.Fatalf("正确样本不该报问题：%v", got)
	}
	commented := `{* 例：{{include "../partials/nope.html"}} *}`
	if got := mailGuardRefProblems(sample, commented); len(got) != 0 {
		t.Fatalf("注释里的示例不该被算作引用：%v", got)
	}
}

func TestMailGuardNoStaleLegacyPage(t *testing.T) {
	files := mailGuardTemplates(t)
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := mailGuardStripComments(string(raw))
		if strings.Contains(src, mailSplitLegacyPath) {
			t.Errorf("%s 仍在引用旧路径 %s（拆页后应指向 /admin/mail/contacts 或 /admin/mail/campaigns）",
				f, mailSplitLegacyPath)
		}
		if strings.Contains(src, mailGuardRetiredTemplate) {
			t.Errorf("%s 仍引用已拆掉的模板 %s", f, mailGuardRetiredTemplate)
		}
	}
	if _, err := os.Stat(filepath.Join(mailGuardTemplateDir, mailGuardRetiredTemplate)); err == nil {
		t.Errorf("%s 应随拆页删除：它的两张表已分别落到活动页与联系人页，留着就是第二份真相",
			mailGuardRetiredTemplate)
	}
}

func TestMailGuardEveryRowColspanMatchesItsTableHeader(t *testing.T) {
	files := mailGuardTemplates(t)
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := mailGuardStripComments(string(raw))

		heads := mailGuardTheadRe.FindAllString(src, -1)
		if len(heads) == 0 {
			continue
		}
		cols := map[int]bool{}
		for _, head := range heads {
			cols[len(mailGuardThRe.FindAllString(head, -1))] = true
		}
		for _, m := range mailGuardColspanRe.FindAllStringSubmatch(src, -1) {
			n := 0
			for _, ch := range m[1] {
				n = n*10 + int(ch-'0')
			}
			if !cols[n] {
				t.Errorf("%s 的 colspan=\"%d\" 与任何表头列数都对不上（本文件的表头列数：%v）——"+
					"colspan 必须精确等于列数，否则整表错位", f, n, keysOfIntSet(cols))
			}
			if len(heads) == 1 && n != len(mailGuardThRe.FindAllString(heads[0], -1)) {
				t.Errorf("%s 只有一张表（%d 列），却出现 colspan=\"%d\"", f,
					len(mailGuardThRe.FindAllString(heads[0], -1)), n)
			}
		}
	}
}

func keysOfIntSet(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestMailGuardPageTemplatesDeclareH1(t *testing.T) {
	files := mailGuardTemplates(t)
	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := mailGuardStripComments(string(raw))
		if !strings.Contains(src, "<table") {
			continue
		}
		checked++
		if n := len(mailGuardH1Re.FindAllString(src, -1)); n != 1 {
			t.Errorf("%s 应恰好有 1 个 <h1>，实际 %d 个", f, n)
		}
	}
	if checked == 0 {
		t.Fatal("没有任何含表格的页面模板被检查 —— 判据成了空转")
	}
}

var mailGuardStyleAttrRe = regexp.MustCompile(`style\s*=\s*(?:"([^"]*)"|'([^']*)')`)

var mailGuardInlineStyleBaseline = "word-break:break-all"

func mailGuardStyleValue(m []string) string {
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

func mailGuardInlineStyleProblems(src string) []string {
	var problems []string
	for _, m := range mailGuardStyleAttrRe.FindAllStringSubmatch(mailGuardStripComments(src), -1) {
		v := mailGuardStyleValue(m)
		if v != mailGuardInlineStyleBaseline {
			problems = append(problems, `内联 style="`+v+`"（应改用 CSS 类）`)
		}
	}
	return problems
}

func TestMailGuardNoInlineStyleBeyondBaseline(t *testing.T) {
	files := mailGuardTemplates(t)
	baselineHits := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读模板失败 %s: %v", f, err)
		}
		src := string(raw)
		for _, p := range mailGuardInlineStyleProblems(src) {
			t.Errorf("%s：%s", f, p)
		}
		for _, m := range mailGuardStyleAttrRe.FindAllStringSubmatch(mailGuardStripComments(src), -1) {
			if mailGuardStyleValue(m) == mailGuardInlineStyleBaseline {
				baselineHits++
			}
		}
	}
	if baselineHits > 1 {
		t.Errorf("基线内的内联 style（%s）出现 %d 次 > 1：照抄基线写法同样是新增内联样式",
			mailGuardInlineStyleBaseline, baselineHits)
	}
	t.Logf("扫描 %d 个邮件模板：基线内联 style %d 处", len(files), baselineHits)
}

func TestMailGuardInlineStyleCheckIsNotVacuous(t *testing.T) {
	bad := `<td style="color:red">`
	if got := mailGuardInlineStyleProblems(bad); len(got) != 1 {
		t.Fatalf("坏样本应报 1 个问题，实际 %d 个：%v", len(got), got)
	}
	good := `<td style="word-break:break-all">` + "\n" + `<td class="cell-wrap">`
	if got := mailGuardInlineStyleProblems(good); len(got) != 0 {
		t.Fatalf("基线写法与类名写法都不该报问题：%v", got)
	}
	commented := `{* 例：<td style="color:red"> *}`
	if got := mailGuardInlineStyleProblems(commented); len(got) != 0 {
		t.Fatalf("注释里的示例不该被算作内联样式：%v", got)
	}
	single := `<td style='color:red'>`
	if got := mailGuardInlineStyleProblems(single); len(got) != 1 {
		t.Fatalf("单引号写法同样要抓到，实际 %d 个：%v", len(got), got)
	}
}
