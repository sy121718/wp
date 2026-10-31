package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var pendingFormProtocolConflicts = map[string]int{}

func TestWriteFormsHaveSingleSubmitProtocol(t *testing.T) {
	root := repoRoot(t)
	tplRoot := filepath.Join(root, "internal", "templates")

	formTag := regexp.MustCompile(`(?s)<form\b[^>]*>`)
	hxVerb := regexp.MustCompile(`\bhx-(?:post|put|patch|delete|get)\s*=`)
	oldApi := regexp.MustCompile(`\bdata-write-api\s*=`)

	hits := map[string]int{}
	err := filepath.Walk(tplRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, path)
		for _, tag := range formTag.FindAllString(string(b), -1) {
			if hxVerb.MatchString(tag) && oldApi.MatchString(tag) {
				hits[filepath.ToSlash(rel)]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历模板失败: %v", err)
	}

	for file, n := range hits {
		t.Logf("%s 有 %d 个表单同时挂了 hx-* 与 data-write-api —— 一次点击会被两个基座各接管一次", file, n)
	}
	assertBudget(t, hits, pendingFormProtocolConflicts, "同一个写表单只能挂一套提交声明（htmx 或 json_write）")
}

func TestWriteBaseEntryGuardsOnUrl(t *testing.T) {
	src := readFile(t, filepath.Join(repoRoot(t),
		"internal/templates/static/js/ui/write-base.js"))
	if !strings.Contains(src, "if (!attr(form, submitter, 'data-write-url')) return;") {
		t.Fatalf("write-base.js 的 submit 入口不再以 data-write-url 为空即放行为前提 —— " +
			"这会让页面上的非写表单（筛选 / 搜索）也被接管并 post 到空 URL。" +
			"见该文件 onSubmit 附近。")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}
