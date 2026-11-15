package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var enumsFiles = map[string]string{
	"cart":  "internal/module/cart/enums/cart_enums.go",
	"order": "internal/module/order/enums/order_enums.go",
	"user":  "internal/module/user/enums/user_enums.go",
	"mail":  "internal/module/mail/enums/mail_enums.go",

	"workbench": "internal/module/workbench/enums/workbench_enums.go",
}

func TestEnumsKeysHaveSeedEntries(t *testing.T) {
	root := moduleTestRoot(t)

	enumsRe := regexp.MustCompile(`(?m)^\s*(?:const\s+)?\w+\s*=\s*"([a-z][a-z0-9]*\.(?:msg|err|test)\.[A-Za-z0-9_.]+)"`)
	// public/schema 的位置参数 INSERT：行首是 (id, 'key', ...)。
	seedRe := regexp.MustCompile(`(?m)^\s*\(\s*\d+\s*,\s*'([^']+)'`)

	have := map[string]bool{}
	seedFiles, err := filepath.Glob(filepath.Join(root, "public", "schema", "tables", "*.sql"))
	if err != nil {
		t.Fatalf("枚举 seed 文件失败: %v", err)
	}
	for _, sf := range seedFiles {
		src, rerr := os.ReadFile(sf)
		if rerr != nil {
			t.Fatalf("读取 seed %s 失败: %v", sf, rerr)
		}
		for _, m := range seedRe.FindAllStringSubmatch(string(src), -1) {
			have[m[1]] = true
		}
	}
	if len(have) == 0 {
		t.Fatalf("public/schema 里没扫到任何 key（共 %d 个 .sql）—— 判据或目录结构变了", len(seedFiles))
	}

	for module, enumsFile := range enumsFiles {
		enumsSrc, err := os.ReadFile(filepath.Join(root, enumsFile))
		if err != nil {
			t.Fatalf("读取 %s 的 enums 失败: %v", module, err)
		}
		want := map[string]bool{}
		for _, m := range enumsRe.FindAllStringSubmatch(string(enumsSrc), -1) {
			want[m[1]] = true
		}
		if len(want) == 0 {
			t.Fatalf("%s：enums 里没扫到任何 key，判据可能已失效", module)
		}
		var missing []string
		for k := range want {
			if !have[k] {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s：这些 key 在 enums 里但 seed 里没有词条（用户会直接看到 key）：%s",
				module, strings.Join(missing, ", "))
		}
	}
}

func moduleTestRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd 失败: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("未找到仓库根（go.mod）")
	return ""
}
