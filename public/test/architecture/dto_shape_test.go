package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var pendingDTOFiles = map[string]bool{}

var dtoFileName = regexp.MustCompile(`^[a-z0-9_]+_(req|resp)\.go$`)

func TestDTOFileNamesAreReqOrResp(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	dirs, err := filepath.Glob(filepath.Join(root, "internal/module/*/dto"))
	if err != nil {
		t.Fatalf("枚举 dto 目录失败: %v", err)
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel, err := filepath.Rel(root, filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("计算相对路径失败: %v", err)
			}
			rel = filepath.ToSlash(rel)
			if dtoFileName.MatchString(name) {
				continue
			}
			seen[rel] = true
			if !pendingDTOFiles[rel] {
				t.Errorf("%s：dto 目录只允许 <域>_req.go / <域>_resp.go —— 把入参并进 _req.go、回执并进 _resp.go", rel)
			}
		}
	}
	for rel := range pendingDTOFiles {
		if !seen[rel] {
			t.Errorf("pendingDTOFiles 登记了 %s，但该文件已不存在（或已合规）—— 请删掉这条登记", rel)
		}
	}
}
