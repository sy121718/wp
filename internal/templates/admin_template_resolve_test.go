package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var tplRefRe = regexp.MustCompile(`\{\{\s*(?:extends|include|import)\s+"([^"]+)"`)

func adminTemplateRefProblems(path string, src string) []string {
	var problems []string
	dir := filepath.Dir(filepath.FromSlash(path))
	for _, m := range tplRefRe.FindAllStringSubmatch(stripJetComments(src), -1) {
		rel := m[1]
		if !strings.HasSuffix(rel, ".html") {
			rel += ".html"
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(target); err != nil {
			problems = append(problems,
				"引用 "+m[1]+" 的落点不存在："+filepath.ToSlash(target))
		}
	}
	return problems
}

func TestAdminTemplateReferencesResolve(t *testing.T) {
	files := adminTemplateFiles(t)
	if len(files) == 0 {
		t.Fatal("没扫到 admin 模板 —— 判据成了空转（目录结构变了？）")
	}
	checked := 0
	for _, f := range files {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			t.Fatalf("读 %s 失败：%v", f, err)
		}
		checked += len(tplRefRe.FindAllStringSubmatch(stripJetComments(string(raw)), -1))
		for _, p := range adminTemplateRefProblems(f, string(raw)) {
			t.Errorf("%s：%s", f, p)
		}
	}
	if checked == 0 {
		t.Fatal("没扫到任何 extends/import/include —— 判据成了空转（正则或写法变了？）")
	}
	t.Logf("检查了 %d 个模板、%d 处引用", len(files), checked)
}

func TestAdminTemplateReferenceCheckIsNotVacuous(t *testing.T) {

	bad := `{{extends "../layout.html"}}` + "\n" + `{{include "../fragments/seo_score.html"}}`
	if got := adminTemplateRefProblems("admin/content/article_edit.html", bad); len(got) != 1 {
		t.Fatalf("坏样本应报 1 个问题，实际 %d 个：%v", len(got), got)
	}

	good := `{{extends "../layout.html"}}` + "\n" +
		`{{include "../../fragments/seo_score.html"}}` + "\n" +
		`{{import "../partials/rich_editor.html"}}`
	if got := adminTemplateRefProblems("admin/content/article_edit.html", good); len(got) != 0 {
		t.Fatalf("正确样本不该报问题：%v", got)
	}

	commented := `{* 例：{{include "../fragments/nope.html"}} *}`
	if got := adminTemplateRefProblems("admin/content/x.html", commented); len(got) != 0 {
		t.Fatalf("注释里的示例不该被算作引用：%v", got)
	}
}
