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

// TestWriteBaseEntryGuardsOnUrl 锁住「写基座只认领声明过的写表单」：
// data-write-url 旧协议已退役，现行契约是 data-write 认领标记 + HTMX 提交；
// 基座不得接管普通筛选 / 搜索表单，也不得自带 URL 执行器。
func TestWriteBaseEntryGuardsOnUrl(t *testing.T) {
	src := readFile(t, filepath.Join(repoRoot(t),
		"pkg/view/ui/assets/js/ui/write-base.js"))
	if strings.Contains(src, "data-write-url") {
		t.Fatal("write-base.js 仍引用已退役的 data-write-url —— 旧执行器不应复活")
	}
	if !strings.Contains(src, "data-write'") && !strings.Contains(src, "data-write\"") {
		t.Fatal("write-base.js 不再按 data-write 认领标记判断写表单 —— 会误接管普通筛选/搜索表单")
	}
	if !strings.Contains(src, "document.addEventListener('submit'") {
		t.Fatal("write-base.js 缺少 submit 拦截入口：预览态与未认领表单将发出真实请求")
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
