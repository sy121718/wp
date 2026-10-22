package architecture

// write_form_protocol_test.go — 同一个写表单不能同时挂两套提交声明。
//
// 背景：写动作现在有**两个**基座并存 ——
//
//	· 旧：ui/json_write.js —— 表单/按钮挂 `data-write-api`，被它接管后走 fetch；
//	· 新：htmx —— 表单挂 `hx-post`，由 htmx 发 formdata。
//
// 两者的入口都是 submit 事件：
//   - json_write.js 的判据是 `if (!apiOf(form, submitter)) return;` —— 没有 data-write-api
//     就完全不接管；
//   - htmx 的判据是存在 hx-post[hx-get...] 属性就接管。
//
// 所以**同一个表单同时挂两者**时，两个基座都会认为自己该接管：json_write 会
// preventDefault 之后发 fetch，htmx 也会发自己的请求 —— 一次点击发两个请求。
// 对写动作来说这不是「多发一次」，是「建两条记录 / 删两次」（仓库里踩过：
// product_create_form 的注释写着「fetch 与 htmx 同时提交 = 建两个商品」）。
//
// 门禁只禁止**同一个表单节点**上的冲突，不禁止一份模板里同时出现两类属性 ——
// 一张列表页完全可以一半表单迁了、一半没迁，那是迁移过程中的正常状态。

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

// TestOldBaseEntryGuardsOnApiAttr 旧基座的入口必须继续以 data-write-api 为前提。
//
// 这是「双声明才会双提交」这条推论的前提：如果 json_write.js 哪天改成「有 hx-post 也接管」，
// 上面那道门禁挡不住（它只看模板声明），而症状会是**所有** htmx 表单都双提交。
// 用源码断言把这条前提钉住，比注释可靠。
func TestOldBaseEntryGuardsOnApiAttr(t *testing.T) {
	src := readFile(t, filepath.Join(repoRoot(t),
		"internal/templates/static/js/ui/json_write.js"))
	if !strings.Contains(src, "if (!apiOf(form, submitter)) { return; }") {
		t.Fatalf("json_write.js 的 submit 入口不再以 apiOf 为空即放行为前提 —— " +
			"这会让所有 htmx 表单被它一并接管（双提交）。见该文件 addEventListener('submit') 附近。")
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
