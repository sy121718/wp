package architecture

// write_form_protocol_test.go — 同一个写表单只能有一套提交声明。
//
// 历史背景（已解决）：写动作曾经有**两个**基座并存 —— 旧的 ui/write-base.js
//（表单挂 data-write-api，接管后走 fetch）与 htmx（表单挂 hx-post 发 formdata）。
// 两者的入口都是 submit 事件，所以同一个表单同时挂两者会**一次点击发两个请求** ——
// 对写动作不是「多发一次」而是「建两条记录 / 删两次」（仓库踩过：
// product_create_form 的注释写着「fetch 与 htmx 同时提交 = 建两个商品」）。
//
// **2026-11 批次 2 之后只剩一个基座**（ui/write-base.js），双基座冲突在结构上不再可能。
// 这道门禁仍然保留，因为「写表单上别挂 hx-post」这条**依然要守**：
// htmx 照样会在自己接管的表单上发请求，而写基座也会在 submit 时接管 —— 同一个坑，
// 只是换了一方。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pendingFormProtocolConflicts 已知的同表单双声明（迁移台账，收一个删一个）。
var pendingFormProtocolConflicts = map[string]int{}

// TestWriteFormsHaveSingleSubmitProtocol 扫模板，要求同一个 <form> 里不出现两套提交声明。
func TestWriteFormsHaveSingleSubmitProtocol(t *testing.T) {
	root := repoRoot(t)
	tplRoot := filepath.Join(root, "internal", "templates")

	// 只匹配 form 开始标签（跨行），取出它的属性串再判断。
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

// TestWriteBaseEntryGuardsOnUrl 写基座的入口必须以 data-write-url 为前提。
//
// 这是「不会误接管」的前提：基座在 document 上以**捕获阶段**接所有 submit，
// 若入口不检查 data-write-url，页面上的非写表单（筛选、搜索）也会被它 preventDefault
// 并 post 到一个空 URL。用源码断言把这条前提钉住，比注释可靠。
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
