// json_import_boundary_test.go —— JSON 入口边界（任务「显式采用 encoding/json/v2 API」的落地闸门）。
//
// 判据：全仓 Go 代码只能经 `go_wp/pkg/jsonx` 使用 JSON；标准库 encoding/json 家族
// （`encoding/json` / `encoding/json/v2` / `encoding/json/jsontext`）只允许出现在 pkg/jsonx 内部。
//
// 为什么值得一条门禁：v1 与 v2 的可观察行为不同（nil 切片 / nil 映射、HTML 字符、U+2028/2029、
// 重复键、字段名大小写、错误文案），散落直连就是让同一个仓库里并存两套 JSON 语义；而**写错
// 不会有任何编译错误**（两边 API 同名，`json.Marshal` 长相一样）。逐条差异与还原选项见
// pkg/jsonx 包注释。
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

	// 白名单：本包是全仓唯一允许直连标准库 JSON 的地方，每条都要写明理由。
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

	// 反向对账：白名单条目不再命中（文件改名/删除/已改用 jsonx）同样判失败 ——
	// 只增不减的登记表会烂成注释。
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
