// dto_shape_test.go — 全仓 dto 目录的**形状门禁**：一个域只能有 req / resp 两份文件。
//
// 口径（2026-10-18 用户拍板，product 模块已按此收口）：控制面 DTO 目录里，一个业务域最多两份
// 文件 —— `<域>_req.go`（接手的入参）/ `<域>_resp.go`（返回的回执）。别的命名（xxx_dto.go、
// xxx_view.go、xxx_req 之外的一切）都要先判断「这份形状是进还是出」才能定位，同一屏的入参
// 与回执也常常散在不同文件里。
//
// **pending 是必须缩小的清单**：登记的是此刻还没收口的文件，判据双向 ——
// 登记了却已不存在同样失败（收口完不删登记 = 变红），所以它不会退化成注释。
// 新写的 dto 文件一律直接按 req / resp 命名，不要往 pending 里加。
package architecture

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// pendingDTOFiles 还没收口的存量 dto 文件（收到一个删一条）。
var pendingDTOFiles = map[string]bool{
	// 已全部收口（2026-10-18）：新写的 dto 文件一律直接按 <域>_req.go / <域>_resp.go 命名。
}

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
