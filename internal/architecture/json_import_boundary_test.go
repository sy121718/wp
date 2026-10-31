package architecture

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestJSONImportBoundary(t *testing.T) {
	root := repoRoot(t)

	allowed := map[string]string{
		"pkg/jsonx/jsonx.go":       "唯一 JSON 入口：内部 = encoding/json/v2 + jsontext；Number 别名需要 v1 的 json.Number",
		"pkg/jsonx/compat_test.go": "差分测试：必须同时 import v1 与 jsonx 才能逐字节对照",
	}

	isStdJSON := func(p string) bool {
		return p == "encoding/json" || strings.HasPrefix(p, "encoding/json/")
	}

	seen := map[string]bool{}
	var problems []string
	for _, dir := range []string{"cmd", "config", "internal", "pkg", "public", "scripts"} {
		hits, err := scanImports(filepath.Join(root, dir), isStdJSON)
		if err != nil {
			t.Fatalf("扫描 %s 失败：%v", dir, err)
		}
		for _, hit := range hits {
			file, _, _ := strings.Cut(hit, " -> ")
			seen[file] = true
			if _, ok := allowed[file]; ok {
				continue
			}
			problems = append(problems, hit)
		}
	}

	for file, reason := range allowed {
		if !seen[file] {
			problems = append(problems, fmt.Sprintf("白名单条目已无调用点：%s（理由：%s）", file, reason))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("JSON 入口边界被越过（共 %d 处）—— 改用 go_wp/pkg/jsonx：\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}
