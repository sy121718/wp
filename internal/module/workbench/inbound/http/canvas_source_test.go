package workbenchhttp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pageLogicFiles(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举页面源文件失败: %v", err)
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasSuffix(f, "_test_helper.go") {
			continue
		}

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
