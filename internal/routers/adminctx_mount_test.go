package routers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sessionCSRFLine = "builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),"
	mountCall       = "adminctx.PermContextMiddleware(adminAuthzSvc)"

	mountCallSuffix = "adminctx.PermContextMiddleware(adminAuthzSvc))"
)

func TestPermContextMiddlewareMountShape(t *testing.T) {
	raw, err := os.ReadFile("assembly.go")
	if err != nil {
		t.Fatalf("读取 assembly.go 失败: %v", err)
	}
	src := string(raw)
	lines := strings.Split(src, "\n")

	var hits []int
	for i, line := range lines {
		if strings.Contains(line, mountCall) {
			hits = append(hits, i)
		}
	}
	if len(hits) != 2 {
		t.Fatalf("装配层应有且仅有 2 处 %s（/admin 与编辑器组），实际 %d 处：%v\n"+
			"少挂一处 → 那一组页面整片失去侧栏与按钮；多挂一处 → 同一请求装配两次。",
			mountCall, len(hits), hits)
	}
	for _, i := range hits {

		if got := strings.TrimSpace(lines[i]); got != mountCallSuffix {
			t.Errorf("第 %d 行的挂载调用不是约定的整行形态：\n  实际: %q\n  期望: %q",
				i+1, got, mountCallSuffix)
		}
		if i == 0 || strings.TrimSpace(lines[i-1]) != sessionCSRFLine {
			t.Errorf("第 %d 行的挂载点前面不是「Session + CSRF」：\n  实际前一行: %q\n"+
				"顺序变了就等于权限上下文先于会话跑 —— 拿不到 user_id，集合恒为空。",
				i+1, prevLine(lines, i))
		}

		gline, ok := groupWithinThreeLines(lines, i)
		if !ok {
			t.Errorf("第 %d 行的挂载点上方 3 行内找不到 router.Group( —— 它不是挂在 group 链上？", i+1)
			continue
		}

		if !strings.HasPrefix(gline, "a.") || !strings.Contains(gline, "= router.Group(\"") || !strings.HasSuffix(gline, ",") {
			t.Errorf("第 %d 行的挂载点上方那条 group 行不是三行式的首行：%q", i+1, gline)
		}
	}

	if n := strings.Count(src, mountCall); n != 2 {
		t.Errorf("两条链的 authz 实例不一致（%d 处同名调用）—— 同一进程里两份权限上下文会分叉", n)
	}
}

func prevLine(lines []string, i int) string {
	if i == 0 {
		return ""
	}
	return lines[i-1]
}

func groupWithinThreeLines(lines []string, i int) (string, bool) {
	for j := i - 1; j >= 0 && j >= i-3; j-- {
		if strings.Contains(lines[j], "router.Group(") {
			return strings.TrimSpace(lines[j]), true
		}
	}
	return "", false
}

func TestNoShellPermContextCallSites(t *testing.T) {
	root := filepath.Join("..", "..")

	for _, banned := range []string{"shell." + "PermContextMiddleware(", "shell." + "NavTree("} {
		var offenders []string
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", "vendor", "tmp", ".scratch":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			if strings.Contains(string(b), banned) {
				offenders = append(offenders, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历仓库失败: %v", err)
		}
		if len(offenders) > 0 {
			t.Errorf("旧入口 %s 仍有 %d 处引用：%v\n"+
				"装配与读取自 task-92 批 3 起在 internal/middleware/adminctx；加转发会让 shell 永远删不掉。",
				banned, len(offenders), offenders)
		}
	}
}
