package templates

// admin_template_resolve_test.go — 模板引用的**落点**守卫（extends / import / include）。
//
// 为什么需要它：这三种引用都相对「当前模板文件所在目录」解析（不是模板根），而模板按后端模块
// 分进子目录之后路径必须跟着变：`../layout.html`、`../partials/x.html`；**`fragments/` 却在模板根下**，
// 从 `admin/<模块>/` 出发要写 `../../fragments/x.html`。
//
// 写错的后果不是编译错误，而是那个页面 500：渲染器先渲到 buffer、失败走 http.Error(500)
// 并丢弃半截内容（见 CLAUDE.md 首节），htmx 档还因 5xx 不 swap 而毫无反应。
// 既有测试打不中它 —— 它们只渲染自己关心的那几页。
//
// 实测代价：一次 admin/ 目录重构把 `admin/content/article_edit.html` 的
// `../fragments/seo_score.html` 留了下来（模板根下的 fragments 被当成了 admin/fragments/），
// content 包 4 条渲染测试红；而当时的「真实 HTTP 抽查」没覆盖带参数的编辑页，所以没抓到。
// 静态落点检查能在**不渲染、不连库**的前提下把这类问题一次报全。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// tplRefRe 抓 {{extends "x"}} / {{include "x" .Ctx}} / {{import "x"}} 的第一个字面量路径参数。
// 动态路径（变量形式）不匹配，也就不会被误报 —— 本判据只认字面量。
var tplRefRe = regexp.MustCompile(`\{\{\s*(?:extends|include|import)\s+"([^"]+)"`)

// adminTemplateRefProblems 检查一份模板源码的引用落点，返回问题描述（空 = 全部可解析）。
//
// 抽成纯函数是为了能用坏样本钉住判据本身（见 TestAdminTemplateReferenceCheckIsNotVacuous）——
// 与 admin_template_integrity_test.go 的做法一致：没有自检的门禁很容易退化成空转。
func adminTemplateRefProblems(path string, src string) []string {
	var problems []string
	dir := filepath.Dir(filepath.FromSlash(path))
	for _, m := range tplRefRe.FindAllStringSubmatch(stripJetComments(src), -1) {
		rel := m[1]
		if !strings.HasSuffix(rel, ".html") {
			rel += ".html" // Jet 的 WithTemplateNameExtensions 允许省略后缀
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Stat(target); err != nil {
			problems = append(problems,
				"引用 "+m[1]+" 的落点不存在："+filepath.ToSlash(target))
		}
	}
	return problems
}

// TestAdminTemplateReferencesResolve admin/ 下每个模板的 extends / import / include 都要有落点。
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

// TestAdminTemplateReferenceCheckIsNotVacuous 用坏样本钉住判据本身。
func TestAdminTemplateReferenceCheckIsNotVacuous(t *testing.T) {
	// 坏样本：从 admin/content/ 引用 ../fragments/x.html —— fragments 在模板根下，少了一层。
	// 这正是实测漏过的那一处（article_edit.html 的 seo_score 片段）。
	bad := `{{extends "../layout.html"}}` + "\n" + `{{include "../fragments/seo_score.html"}}`
	if got := adminTemplateRefProblems("admin/content/article_edit.html", bad); len(got) != 1 {
		t.Fatalf("坏样本应报 1 个问题，实际 %d 个：%v", len(got), got)
	}
	// 正确样本：extends 与 include 都要能解析到（render 期才知道的那种错，这里静态就能挡）。
	good := `{{extends "../layout.html"}}` + "\n" +
		`{{include "../../fragments/seo_score.html"}}` + "\n" +
		`{{import "../partials/rich_editor.html"}}`
	if got := adminTemplateRefProblems("admin/content/article_edit.html", good); len(got) != 0 {
		t.Fatalf("正确样本不该报问题：%v", got)
	}
	// 注释里的示例不算引用（模板注释里常写示例路径，照字面统计必然误报）。
	commented := `{* 例：{{include "../fragments/nope.html"}} *}`
	if got := adminTemplateRefProblems("admin/content/x.html", commented); len(got) != 0 {
		t.Fatalf("注释里的示例不该被算作引用：%v", got)
	}
}
