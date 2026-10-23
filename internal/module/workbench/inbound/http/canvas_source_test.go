package workbenchhttp

// page_source_test_helper.go — 源码形状守卫的读取口（仅测试用，但放在非 _test 文件里
// 是为了让多个 _test.go 共用同一个实现；文件名带 _test_helper 后缀不影响构建）。
//
// 背景：有几个守卫测试断言「源码里存在某个出口形状」（比如 workbenchBlock 的失败分支
// 必须是 renderWorkbenchPageError 而不是裸 c.String）。它们原先读固定的
// workbench_page.go —— 那在页面逻辑拆成 4 个文件之后立刻失效：守卫扫不到目标，
// 报「写法变了？这条守卫已失效」，而**代码本身完全没问题**。
//
// 判据应当是「这一批文件里存在这个形状」，而不是「它恰好在哪个文件里」：
// 文件划分是阅读便利，不是契约。所以这里把整个包的页面逻辑源码拼起来供测试断言。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pageLogicFiles 页面逻辑相关的源文件（**不含** _test.go）。
//
// 用 glob 而不是硬编码清单：再拆/再合文件时守卫自动跟上，
// 不需要每个守卫测试各自维护一份文件名列表。
func pageLogicFiles(t *testing.T) []string {
	t.Helper()
	// glob 覆盖本包的页面逻辑文件（*_handle.go 是新命名规范，workbench_page.go 是遗留原名）。
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举页面源文件失败: %v", err)
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasSuffix(f, "_test_helper.go") {
			continue
		}
		// 只看页面逻辑：路由与错误归口各自独立，不参与页面出口形状的断言。
		if f == "router.go" || f == "workbench_err.go" || f == "workbench_handle.go" {
			continue
		}
		out = append(out, f)
	}
	if len(out) < 3 {
		t.Fatalf("只枚举到 %d 个页面源文件（预期 ≥3）：glob 口径失效，守卫会在空转中通过", len(out))
	}
	return out
}

// pageLogicSource 把整个包的页面逻辑源码拼起来（带文件名分隔，报错时能定位）。
func pageLogicSource(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, f := range pageLogicFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", f, err)
		}
		b.WriteString("// ==== " + f + " ====\n")
		b.Write(data)
		b.WriteString("\n")
	}
	return b.String()
}
