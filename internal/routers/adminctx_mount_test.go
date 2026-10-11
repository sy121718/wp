package routers

// adminctx_mount_test.go — 权限上下文中间件「挂哪儿」的结构门禁（task-92 批 3）。
//
// 为什么不靠运行时断言：漏挂/挂错组不会让任何页面报错 —— 页面照常渲染，只是侧栏与按钮
// 全空（权限集是空 map，模板的可见性判断全部为假）。这类故障只能靠**看挂载点形状**发现。
//
// 判据分三块（全部是源码级，不需要数据库）：
//  1. 本文件所在包（装配层）里 adminctx.PermContextMiddleware 恰好出现 2 次，
//     且两次都紧跟 Session + CSRF 中间件、用的是同一个 authz 服务实例；
//  2. 全仓（含测试）再没有 shell.PermContextMiddleware 的调用；
//  3. 全仓没有 shell.NavTree 的调用（读取器已删除，遗留调用会编译失败，这里是第二道栅栏）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mountContext 是挂载点必须满足的相邻形状：两条页面组链都写成
// `router.Group(<前缀>, Session, CSRF, adminctx.PermContextMiddleware(<svc>))` 的三行式。
const (
	sessionCSRFLine = "builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),"
	mountCall       = "adminctx.PermContextMiddleware(adminAuthzSvc)"
	// mountCallSuffix 是挂载行的**整行**形态（含 group 调用的收尾右括号）——
	// 断言整行相等：缩进、多余参数、换成别的服务都会改变它的字节。
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
		// 断言整行相等而不是 Contains：缩进/多余参数/把参数换成别的服务都会改变这一行的字节。
		if got := strings.TrimSpace(lines[i]); got != mountCallSuffix {
			t.Errorf("第 %d 行的挂载调用不是约定的整行形态：\n  实际: %q\n  期望: %q",
				i+1, got, mountCallSuffix)
		}
		if i == 0 || strings.TrimSpace(lines[i-1]) != sessionCSRFLine {
			t.Errorf("第 %d 行的挂载点前面不是「Session + CSRF」：\n  实际前一行: %q\n"+
				"顺序变了就等于权限上下文先于会话跑 —— 拿不到 user_id，集合恒为空。",
				i+1, prevLine(lines, i))
		}
		// router.Group( 在三行式里位于挂载点上方第 2 行（允许中间夹一行注释）。
		gline, ok := groupWithinThreeLines(lines, i)
		if !ok {
			t.Errorf("第 %d 行的挂载点上方 3 行内找不到 router.Group( —— 它不是挂在 group 链上？", i+1)
			continue
		}
		// 该 group 行必须是「赋值给本装配结构的 router.Group("..",」形态（三行式的第一行），
		// 否则中间件可能被挂到了一个临时表达式或别的链上。
		if !strings.HasPrefix(gline, "a.") || !strings.Contains(gline, "= router.Group(\"") || !strings.HasSuffix(gline, ",") {
			t.Errorf("第 %d 行的挂载点上方那条 group 行不是三行式的首行：%q", i+1, gline)
		}
	}
	// 两条链必须共用同一个 authz 实例：否则一条链上的页面按 A 权限渲染、另一条按 B。
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

// TestNoShellPermContextCallSites 全仓源码里不得再出现旧入口的调用。
// 旧的 shell.PermContextMiddleware 已随本批删除：留任何一处都是编译错误；
// 这条断言的价值在于把「有人把转发加回来」挡在评审之外（转发会让迁移名存实亡）。
func TestNoShellPermContextCallSites(t *testing.T) {
	root := filepath.Join("..", "..")
	// 字面量拼出来而不是写死：本文件自己就在扫「哪些文件提到旧入口」，
	// 写死会让这个测试把自己抓成违规（实测踩过）。
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
